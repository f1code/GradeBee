// jobs_list.go handles GET /jobs — returns the authenticated user's upload
// jobs grouped by status (active, failed, done).
package handler

import (
	"errors"
	"net/http"
	"sort"

	"github.com/clerk/clerk-sdk-go/v2"
)

// JobListResponse groups jobs by their processing state.
type JobListResponse struct {
	Active []VoiceNoteJob `json:"active"`
	Failed []VoiceNoteJob `json:"failed"`
	Done   []VoiceNoteJob `json:"done"`
}

func handleJobList(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	userID := claims.Subject

	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		log.Error("jobs list: get queue", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "queue unavailable"})
		return
	}

	jobs, err := queue.ListJobs(r.Context(), userID)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("jobs list: list jobs", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list jobs"})
		return
	}

	resp := JobListResponse{
		Active: []VoiceNoteJob{},
		Failed: []VoiceNoteJob{},
		Done:   []VoiceNoteJob{},
	}

	for _, j := range jobs {
		switch j.Status {
		case JobStatusDone:
			resp.Done = append(resp.Done, j)
		case JobStatusFailed:
			resp.Failed = append(resp.Failed, j)
		default:
			resp.Active = append(resp.Active, j)
		}
	}

	sortDesc := func(s []VoiceNoteJob) {
		sort.Slice(s, func(i, j int) bool {
			return s[i].CreatedAt.After(s[j].CreatedAt)
		})
	}
	sortDesc(resp.Active)
	sortDesc(resp.Failed)
	sortDesc(resp.Done)

	log.Info("jobs list", "user_id", userID,
		"active", len(resp.Active), "failed", len(resp.Failed), "done", len(resp.Done))
	writeJSON(w, http.StatusOK, resp)
}
