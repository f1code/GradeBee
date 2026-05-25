// feedback_handler.go handles POST /api/feedback for explicit thumbs ratings
// on generated reports and auto-extracted notes.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// submitFeedbackRequest is the JSON body for POST /feedback.
type submitFeedbackRequest struct {
	ArtifactType string  `json:"artifact_type"` // 'report' | 'note'
	ArtifactID   int64   `json:"artifact_id"`
	Rating       string  `json:"rating"`  // 'up' | 'down'
	Comment      *string `json:"comment"` // optional
}

// submitFeedbackResponse is the JSON response for POST /feedback.
type submitFeedbackResponse struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
}

func handleSubmitFeedback(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var req submitFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.ArtifactType != "report" && req.ArtifactType != "note" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact_type must be 'report' or 'note'"})
		return
	}
	if req.Rating != "up" && req.Rating != "down" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rating must be 'up' or 'down'"})
		return
	}
	if req.ArtifactID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact_id is required"})
		return
	}

	ctx := r.Context()

	// Verify ownership — the caller must own the artifact's student.
	var studentID int64
	switch req.ArtifactType {
	case "report":
		rpt, err := serviceDeps.GetReportRepo().GetByID(ctx, req.ArtifactID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		studentID = rpt.StudentID
	case "note":
		n, err := serviceDeps.GetNoteRepo().GetByID(ctx, req.ArtifactID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		studentID = n.StudentID
	}

	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(ctx, studentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": req.ArtifactType + " not found"})
		return
	}

	// Insert the feedback row.
	id, err := serviceDeps.GetFeedbackRepo().Insert(ctx, ArtifactFeedback{
		ArtifactType: req.ArtifactType,
		ArtifactID:   req.ArtifactID,
		Rating:       req.Rating,
		Signal:       "explicit",
		Comment:      req.Comment,
		UserID:       userID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save feedback"})
		return
	}

	// Sentry dual-write: only on explicit thumbs-down.
	if req.Rating == "down" {
		var comment string
		if req.Comment != nil {
			comment = *req.Comment
		}
		captureFeedback(ctx, feedbackEvent{
			UserID:   userID,
			ReportID: func() int64 {
				if req.ArtifactType == "report" {
					return req.ArtifactID
				}
				return 0
			}(),
			Rating:  "thumbs_down",
			Comment: comment,
		})
	}

	slog.InfoContext(ctx, "feedback submitted",
		"artifact_type", req.ArtifactType,
		"artifact_id", req.ArtifactID,
		"rating", req.Rating,
		"user_id", userID,
	)

	writeJSON(w, http.StatusCreated, submitFeedbackResponse{
		ID:        id,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
}
