package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp JobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Active) != 2 {
		t.Errorf("active = %d, want 2", len(resp.Active))
	}
	if len(resp.Done) != 1 {
		t.Errorf("done = %d, want 1", len(resp.Done))
	}
	if len(resp.Failed) != 1 {
		t.Errorf("failed = %d, want 1", len(resp.Failed))
	}
}

func TestJobList_EmptyUser(t *testing.T) {
	queue := newStubVoiceNoteQueue()

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "nobody")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp JobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Active) != 0 || len(resp.Failed) != 0 || len(resp.Done) != 0 {
		t.Errorf("expected all empty arrays, got active=%d failed=%d done=%d",
			len(resp.Active), len(resp.Failed), len(resp.Done))
	}
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
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Active) != 2 {
		t.Fatalf("active = %d, want 2", len(resp.Active))
	}
	// Newest first = uploadID 2
	if resp.Active[0].UploadID != 2 {
		t.Errorf("first active uploadID = %d, want 2 (newest first)", resp.Active[0].UploadID)
	}
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp jobRetryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RetriedCount != 2 {
		t.Errorf("retriedCount = %d, want 2", resp.RetriedCount)
	}

	// Failed jobs should now be queued.
	f1, err := queue.GetJob(context.TODO(), voiceNoteKey("u1", 2))
	if err != nil {
		t.Fatal(err)
	}
	if f1.Status != JobStatusQueued {
		t.Errorf("failed1 status = %q, want queued", f1.Status)
	}
	if f1.Error != "" {
		t.Errorf("failed1 error = %q, want empty", f1.Error)
	}

	// Done job should be unchanged.
	dj, err := queue.GetJob(context.TODO(), voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if dj.Status != JobStatusDone {
		t.Errorf("done-job status = %q, want done", dj.Status)
	}
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp jobRetryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RetriedCount != 0 {
		t.Errorf("retriedCount = %d, want 0", resp.RetriedCount)
	}
}
