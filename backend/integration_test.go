package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestIntegration_PublishToNoteCreation tests the full flow with a real NATS
// server (embedded) and stubbed external services.
func TestIntegration_PublishToNoteCreation(t *testing.T) {
	queue := newTestQueue(t)
	nc := &stubNoteCreator{
		results: []*CreateNoteResponse{
			{DocID: "doc1", DocURL: "https://docs.google.com/1"},
			{DocID: "doc2", DocURL: "https://docs.google.com/2"},
		},
	}

	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("fake audio bytes")),
			fileName:     "2026-03-22-recording.m4a",
		},
		transcriber: &stubTranscriber{result: "Alice did great. Bob needs work."},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []classGroup{{Name: "Math", Students: []student{{Name: "Alice"}, {Name: "Bob"}}}},
		},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-03-22",
			Students: []MatchedStudent{
				{Name: "Alice", Class: "Math", Summary: "Did great", Confidence: 0.9},
				{Name: "Bob", Class: "Math", Summary: "Needs work", Confidence: 0.8},
			},
		}},
		noteCreator: nc,
		uploadQueue: queue,
		metadata:    &gradeBeeMetadata{NotesID: "notes-folder"},
	}

	ctx := context.Background()

	job := UploadJob{
		UserID:    "int-user",
		FileID:    "int-file",
		FileName:  "2026-03-22-recording.m4a",
		MimeType:  "audio/mp4",
		Source:    "web",
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := queue.GetJob(ctx, "int-user", "int-file")
	if err != nil {
		t.Fatalf("get job after publish: %v", err)
	}
	if got.Status != JobStatusQueued {
		t.Fatalf("status after publish = %q, want queued", got.Status)
	}

	if err := processUploadJob(ctx, d, "int-user", "int-file"); err != nil {
		t.Fatalf("process: %v", err)
	}

	got, err = queue.GetJob(ctx, "int-user", "int-file")
	if err != nil {
		t.Fatalf("get job after process: %v", err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want done", got.Status)
	}
	if len(got.NoteURLs) != 2 {
		t.Errorf("noteUrls = %v, want 2 items", got.NoteURLs)
	}
	if len(nc.calls) != 2 {
		t.Errorf("note creator calls = %d, want 2", len(nc.calls))
	}
}

func TestIntegration_PublishToFailure(t *testing.T) {
	queue := newTestQueue(t)

	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("audio")),
			fileName:     "test.m4a",
		},
		transcriber: &stubTranscriber{err: io.ErrUnexpectedEOF},
		roster:      &stubRoster{},
		uploadQueue: queue,
		metadata:    &gradeBeeMetadata{NotesID: "notes-id"},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{
		UserID: "int-user", FileID: "fail-file", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	err := processUploadJob(ctx, d, "int-user", "fail-file")
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "int-user", "fail-file")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.FailedAt == nil {
		t.Error("failedAt should be set")
	}
}

func TestIntegration_RetryAfterFailure(t *testing.T) {
	queue := newTestQueue(t)

	failingTranscriber := &stubTranscriber{err: io.ErrUnexpectedEOF}
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("audio")),
			fileName:     "test.m4a",
		},
		transcriber: failingTranscriber,
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date:     "2026-01-01",
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", Summary: "ok", Confidence: 0.9}},
		}},
		noteCreator: nc,
		uploadQueue: queue,
		metadata:    &gradeBeeMetadata{NotesID: "notes-id"},
	}

	old := serviceDeps
	serviceDeps = d
	t.Cleanup(func() { serviceDeps = old })

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{
		UserID: "int-user", FileID: "retry-file", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// First attempt fails.
	if err := processUploadJob(ctx, d, "int-user", "retry-file"); err == nil {
		t.Fatal("expected error on first attempt")
	}
	got, err := queue.GetJob(ctx, "int-user", "retry-file")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}

	// Fix the transcriber and reset driveStore body.
	failingTranscriber.err = nil
	failingTranscriber.result = "transcript"
	d.driveStore = &stubDriveStore{
		downloadBody: io.NopCloser(strings.NewReader("audio")),
		fileName:     "test.m4a",
	}

	// Retry via handler.
	req := clerkCtx(httptest.NewRequest(http.MethodPost, "/jobs/retry", http.NoBody), "int-user")
	rec := httptest.NewRecorder()
	handleJobRetry(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var retryResp jobRetryResponse
	if err := json.NewDecoder(rec.Body).Decode(&retryResp); err != nil {
		t.Fatalf("decode retry resp: %v", err)
	}
	if retryResp.RetriedCount != 1 {
		t.Errorf("retriedCount = %d, want 1", retryResp.RetriedCount)
	}

	// Process the retried job.
	if err := processUploadJob(ctx, d, "int-user", "retry-file"); err != nil {
		t.Fatalf("second process: %v", err)
	}

	got, err = queue.GetJob(ctx, "int-user", "retry-file")
	if err != nil {
		t.Fatalf("get job after retry: %v", err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status after retry = %q, want done", got.Status)
	}
}

func TestIntegration_ListJobsDuringProcessing(t *testing.T) {
	queue := newTestQueue(t)
	ctx := context.Background()

	// Job 1: done.
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f-done", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	doneJob, err := queue.GetJob(ctx, "u1", "f-done")
	if err != nil {
		t.Fatal(err)
	}
	doneJob.Status = JobStatusDone
	doneJob.NoteURLs = []string{"https://docs.google.com/document/d/doc1/edit"}
	if err := queue.UpdateJob(ctx, *doneJob); err != nil {
		t.Fatal(err)
	}

	// Job 2: failed.
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f-failed", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	failedJob, err := queue.GetJob(ctx, "u1", "f-failed")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	failedJob.Status = JobStatusFailed
	failedJob.Error = "boom"
	failedJob.FailedAt = &now
	if err := queue.UpdateJob(ctx, *failedJob); err != nil {
		t.Fatal(err)
	}

	// Job 3: still queued.
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f-queued", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	old := serviceDeps
	serviceDeps = &mockDepsAll{uploadQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp jobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Active) != 1 {
		t.Errorf("active = %d, want 1", len(resp.Active))
	}
	if len(resp.Failed) != 1 {
		t.Errorf("failed = %d, want 1", len(resp.Failed))
	}
	if len(resp.Done) != 1 {
		t.Errorf("done = %d, want 1", len(resp.Done))
	}
}
