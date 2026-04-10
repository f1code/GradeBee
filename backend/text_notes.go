// text_notes.go handles POST /text-notes/upload — accepts pasted text,
// creates a voice_notes row (with no audio file), and dispatches a job
// that skips transcription and goes straight to extraction.
package handler

import (
	"encoding/json"
	"net/http"
	"time"
)

const maxTextSize = 50 * 1024 // 50 KB

type textNotesRequest struct {
	Text string `json:"text"`
}

func handleTextNotesUpload(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	r.Body = http.MaxBytesReader(w, r.Body, int64(maxTextSize)+1024) // allow for JSON envelope
	var req textNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
		return
	}
	if len(req.Text) > maxTextSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text too large (max 50KB)"})
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()

	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		log.Error("text-notes: queue unavailable", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "service unavailable"})
		return
	}

	// Create a voice_notes row with no audio file.
	repo := serviceDeps.GetVoiceNoteRepo()
	upload, err := repo.Create(ctx, userID, "pasted-text", "")
	if err != nil {
		log.Error("text-notes: create record failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
		return
	}

	err = publishOrCleanup(ctx, queue, VoiceNoteJob{
		UserID:     userID,
		UploadID:   upload.ID,
		FileName:   "pasted-text",
		Source:     "text",
		Transcript: req.Text,
		Status:     JobStatusQueued,
		CreatedAt:  time.Now(),
	},
		func() { _ = repo.Delete(ctx, upload.ID) }, //nolint:errcheck // best-effort cleanup
	)
	if err != nil {
		log.Error("text-notes: dispatch failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process"})
		return
	}

	log.Info("text-notes upload dispatched", "user_id", userID, "upload_id", upload.ID)
	writeJSON(w, http.StatusOK, UploadResponse{
		UploadID: upload.ID,
		FileName: "pasted-text",
	})
}
