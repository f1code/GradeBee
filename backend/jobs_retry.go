// jobs_retry.go handles POST /jobs/retry — resets all failed jobs for the
// authenticated user back to "queued" and republishes them for processing.
package handler

import (
	"errors"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
)

type jobRetryResponse struct {
	RetriedCount int `json:"retriedCount"`
}

func handleJobRetry(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	userID := claims.Subject

	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		log.Error("jobs retry: get queue", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "queue unavailable"})
		return
	}

	ctx := r.Context()

	jobs, err := queue.ListJobs(ctx, userID)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("jobs retry: list jobs", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list jobs"})
		return
	}

	retried := 0
	for _, j := range jobs {
		if j.Status != JobStatusFailed {
			continue
		}
		j.Error = ""
		j.FailedAt = nil
		j.Status = JobStatusQueued
		if err := queue.Publish(ctx, j); err != nil {
			log.Error("jobs retry: republish failed", "upload_id", j.UploadID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retry job"})
			return
		}
		retried++
	}

	log.Info("jobs retry", "user_id", userID, "retried_count", retried)
	writeJSON(w, http.StatusOK, jobRetryResponse{RetriedCount: retried})
}
