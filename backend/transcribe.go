// transcribe.go handles the POST /transcribe endpoint that downloads an audio
// file from Google Drive and sends it to OpenAI Whisper for transcription.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
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

	transcript, err := transcriber.Transcribe(ctx, fileName, resp.Body)
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
