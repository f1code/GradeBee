// jobs_dismiss.go handles DELETE /jobs — dismisses completed jobs for the
// authenticated user. Accepts a JSON body with file IDs to dismiss.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
)

type dismissRequest struct {
	FileIDs []string `json:"fileIds"`
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.FileIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fileIds required"})
		return
	}

	queue, err := serviceDeps.GetUploadQueue()
	if err != nil {
		log.Error("jobs dismiss: get queue", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "queue unavailable"})
		return
	}

	dismissed := 0
	for _, fileID := range req.FileIDs {
		job, err := queue.GetJob(r.Context(), userID, fileID)
		if err != nil {
			continue // not found, skip
		}
		// Only allow dismissing done or failed jobs.
		if job.Status != JobStatusDone && job.Status != JobStatusFailed {
			continue
		}
		if err := queue.DeleteJob(r.Context(), userID, fileID); err != nil {
			var ae *apiError
			if errors.As(err, &ae) {
				writeAPIError(w, r, ae)
				return
			}
			log.Error("jobs dismiss: delete job", "error", err, "file_id", fileID)
			continue
		}
		dismissed++
	}

	log.Info("jobs dismissed", "user_id", userID, "dismissed", dismissed, "requested", len(req.FileIDs))
	writeJSON(w, http.StatusOK, map[string]int{"dismissed": dismissed})
}
