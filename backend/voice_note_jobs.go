// voice_note_jobs.go handles the voice note job endpoints:
//   GET  /voice-notes/jobs         — list jobs grouped by status
//   POST /voice-notes/jobs/retry   — retry failed jobs
//   POST /voice-notes/jobs/dismiss — dismiss completed/failed jobs
package handler

import (
	"encoding/json"
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

	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		log.Error("jobs dismiss: get queue", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "queue unavailable"})
		return
	}

	voiceNoteRepo := serviceDeps.GetVoiceNoteRepo()
	dismissed := 0
	for _, uploadID := range req.UploadIDs {
		job, err := queue.GetJob(r.Context(), voiceNoteKey(userID, uploadID))
		if err != nil {
			continue // not found, skip
		}
		// Only allow dismissing done or failed jobs.
		if job.Status != JobStatusDone && job.Status != JobStatusFailed {
			continue
		}
		if err := queue.DeleteJob(r.Context(), voiceNoteKey(userID, uploadID)); err != nil {
			var ae *apiError
			if errors.As(err, &ae) {
				writeAPIError(w, r, ae)
				return
			}
			log.Error("jobs dismiss: delete job", "error", err, "upload_id", uploadID)
			continue
		}
		// Mark upload as processed so the file enters the cleanup window.
		if voiceNoteRepo != nil {
			if err := voiceNoteRepo.MarkProcessed(r.Context(), uploadID); err != nil {
				log.Warn("jobs dismiss: mark processed", "error", err, "upload_id", uploadID)
			}
		}
		dismissed++
	}

	log.Info("jobs dismissed", "user_id", userID, "dismissed", dismissed, "requested", len(req.UploadIDs))
	writeJSON(w, http.StatusOK, map[string]int{"dismissed": dismissed})
}
