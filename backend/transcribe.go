// transcribe.go handles the POST /transcribe endpoint that downloads an audio
// file from Google Drive and sends it to OpenAI Whisper for transcription.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type transcribeRequest struct {
	FileID string `json:"fileId"`
}

type transcribeResponse struct {
	FileID     string `json:"fileId"`
	Transcript string `json:"transcript"`
}

func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req transcribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid 'fileId'"})
		return
	}

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("transcribe failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Download audio from Drive.
	resp, err := svc.Drive.Files.Get(req.FileID).Context(ctx).Download()
	if err != nil {
		log.Error("transcribe: drive download failed", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found on Google Drive"})
		return
	}
	defer resp.Body.Close()

	// Get file name for Whisper (it uses the extension to determine format).
	fileMeta, err := svc.Drive.Files.Get(req.FileID).Fields("name").Context(ctx).Do()
	fileName := "audio.webm" // fallback
	if err == nil && fileMeta.Name != "" {
		fileName = fileMeta.Name
	}

	transcriber, err := serviceDeps.GetTranscriber()
	if err != nil {
		log.Error("transcribe: transcriber init failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "transcription service unavailable"})
		return
	}

	// Build a Whisper prompt from the user's class names to improve recognition.
	prompt := buildClassNamePrompt(ctx, svc, log)

	transcript, err := transcriber.Transcribe(ctx, fileName, resp.Body, prompt)
	if err != nil {
		log.Error("transcribe: whisper failed", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "transcription failed"})
		return
	}

	log.Info("transcribe completed", "user_id", svc.User.UserID, "file_id", req.FileID)
	writeJSON(w, http.StatusOK, transcribeResponse{
		FileID:     req.FileID,
		Transcript: transcript,
	})
}

// buildClassNamePrompt fetches class names from the user's spreadsheet and
// returns a Whisper prompt string. Returns empty string on any failure.
func buildClassNamePrompt(ctx context.Context, svc *googleServices, log *slog.Logger) string {
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil || meta == nil || meta.SpreadsheetID == "" {
		log.Warn("transcribe: could not fetch spreadsheet metadata for prompt", "error", err)
		return ""
	}

	resp, err := svc.Sheets.Spreadsheets.Values.Get(meta.SpreadsheetID, "Students!A:A").Context(ctx).Do()
	if err != nil {
		log.Warn("transcribe: could not read class names for prompt", "error", err)
		return ""
	}

	seen := make(map[string]struct{})
	var names []string
	for i, row := range resp.Values {
		if i == 0 { // skip header
			continue
		}
		if len(row) == 0 {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", row[0]))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return ""
	}
	return "Classes: " + strings.Join(names, ", ")
}
