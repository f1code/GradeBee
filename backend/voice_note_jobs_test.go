package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clerkCtx(r *http.Request, userID string) *http.Request {
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
	return r.WithContext(ctx)
}

// --- List tests ---

func TestJobList_GroupsByStatus(t *testing.T) {
	queue := newStubVoiceNoteQueue()
	now := time.Now()

	queue.jobs[voiceNoteKey("u1", 1)] = VoiceNoteJob{UserID: "u1", UploadID: 1, Status: JobStatusQueued, CreatedAt: now}
	queue.jobs[voiceNoteKey("u1", 2)] = VoiceNoteJob{UserID: "u1", UploadID: 2, Status: JobStatusTranscribing, CreatedAt: now.Add(-1 * time.Minute)}
	queue.jobs[voiceNoteKey("u1", 3)] = VoiceNoteJob{UserID: "u1", UploadID: 3, Status: JobStatusDone, CreatedAt: now.Add(-2 * time.Minute)}
	queue.jobs[voiceNoteKey("u1", 4)] = VoiceNoteJob{UserID: "u1", UploadID: 4, Status: JobStatusFailed, Error: "boom", CreatedAt: now.Add(-3 * time.Minute)}

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var resp JobListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp.Active, 2)
	assert.Len(t, resp.Done, 1)
	assert.Len(t, resp.Failed, 1)
}

func TestJobList_EmptyUser(t *testing.T) {
	queue := newStubVoiceNoteQueue()

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "nobody")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp JobListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp), "decode")
	assert.Empty(t, resp.Active)
	assert.Empty(t, resp.Failed)
	assert.Empty(t, resp.Done)
}

func TestJobList_SortedDescending(t *testing.T) {
	queue := newStubVoiceNoteQueue()
	now := time.Now()

	queue.jobs[voiceNoteKey("u1", 1)] = VoiceNoteJob{UserID: "u1", UploadID: 1, Status: JobStatusQueued, CreatedAt: now.Add(-10 * time.Minute)}
	queue.jobs[voiceNoteKey("u1", 2)] = VoiceNoteJob{UserID: "u1", UploadID: 2, Status: JobStatusQueued, CreatedAt: now}

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	var resp JobListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Active, 2)
	// Newest first = uploadID 2
	assert.Equal(t, int64(2), resp.Active[0].UploadID, "first active should be newest (uploadID 2)")
}

// --- Retry tests ---

func TestJobRetry_RetriesFailedOnly(t *testing.T) {
	queue := newStubVoiceNoteQueue()
	now := time.Now()
	failedAt := now.Add(-5 * time.Minute)

	queue.jobs[voiceNoteKey("u1", 1)] = VoiceNoteJob{UserID: "u1", UploadID: 1, Status: JobStatusDone, CreatedAt: now}
	queue.jobs[voiceNoteKey("u1", 2)] = VoiceNoteJob{UserID: "u1", UploadID: 2, Status: JobStatusFailed, Error: "err1", FailedAt: &failedAt, CreatedAt: now}
	queue.jobs[voiceNoteKey("u1", 3)] = VoiceNoteJob{UserID: "u1", UploadID: 3, Status: JobStatusFailed, Error: "err2", FailedAt: &failedAt, CreatedAt: now}
	queue.jobs[voiceNoteKey("u1", 4)] = VoiceNoteJob{UserID: "u1", UploadID: 4, Status: JobStatusQueued, CreatedAt: now}

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodPost, "/jobs/retry", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobRetry(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var resp jobRetryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 2, resp.RetriedCount)

	// Failed jobs should now be queued.
	f1, err := queue.GetJob(context.TODO(), voiceNoteKey("u1", 2))
	require.NoError(t, err)
	assert.Equal(t, JobStatusQueued, f1.Status)
	assert.Empty(t, f1.Error)

	// Done job should be unchanged.
	dj, err := queue.GetJob(context.TODO(), voiceNoteKey("u1", 1))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, dj.Status)
}

func TestJobRetry_NoFailedJobs(t *testing.T) {
	queue := newStubVoiceNoteQueue()
	queue.jobs[voiceNoteKey("u1", 1)] = VoiceNoteJob{UserID: "u1", UploadID: 1, Status: JobStatusDone}

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodPost, "/jobs/retry", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobRetry(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp jobRetryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 0, resp.RetriedCount)
}
