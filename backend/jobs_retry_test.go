package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
