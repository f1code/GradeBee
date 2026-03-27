// jobs_dismiss.go handles POST /jobs/dismiss — dismisses completed/failed jobs
// for the authenticated user. Also marks uploads as processed so files enter
// the cleanup window.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
)

type dismissRequest struct {
	UploadIDs []int64 `json:"uploadIds"`
}

func handleJobDismiss(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	userID := claims.Subject

	var req dismissRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.UploadIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uploadIds required"})
		return
	}

	queue, err := serviceDeps.GetUploadQueue()
	if err != nil {
		log.Error("jobs dismiss: get queue", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "queue unavailable"})
		return
	}

	uploadRepo := serviceDeps.GetUploadRepo()
	dismissed := 0
	for _, uploadID := range req.UploadIDs {
		job, err := queue.GetJob(r.Context(), userID, uploadID)
		if err != nil {
			continue // not found, skip
		}
		// Only allow dismissing done or failed jobs.
		if job.Status != JobStatusDone && job.Status != JobStatusFailed {
			continue
		}
		if err := queue.DeleteJob(r.Context(), userID, uploadID); err != nil {
			var ae *apiError
			if errors.As(err, &ae) {
				writeAPIError(w, r, ae)
				return
			}
			log.Error("jobs dismiss: delete job", "error", err, "upload_id", uploadID)
			continue
		}
		// Mark upload as processed so the file enters the cleanup window.
		if uploadRepo != nil {
			if err := uploadRepo.MarkProcessed(r.Context(), uploadID); err != nil {
				log.Warn("jobs dismiss: mark processed", "error", err, "upload_id", uploadID)
			}
		}
		dismissed++
	}

	log.Info("jobs dismissed", "user_id", userID, "dismissed", dismissed, "requested", len(req.UploadIDs))
	writeJSON(w, http.StatusOK, map[string]int{"dismissed": dismissed})
}
