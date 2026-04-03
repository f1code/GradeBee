package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

func TestIntegration_PublishToNoteCreation(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	uploadRepo := &UploadRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "int-user", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Bob"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("fake audio bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newTestQueue(t)
	nc := &stubNoteCreator{
		results: []*CreateNoteResponse{
			{NoteID: 1},
			{NoteID: 2},
		},
	}

	d := &mockDepsAll{
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
		studentRepo: studentRepo,
		uploadRepo:  uploadRepo,
	}

	ctx := context.Background()

	job := UploadJob{
		UserID:    "int-user",
		UploadID:  1,
		FilePath:  audioPath,
		FileName:  "2026-03-22-recording.m4a",
		MimeType:  "audio/mp4",
		Source:    "web",
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := queue.GetJob(ctx, "int-user", 1)
	if err != nil {
		t.Fatalf("get job after publish: %v", err)
	}
	if got.Status != JobStatusQueued {
		t.Fatalf("status after publish = %q, want queued", got.Status)
	}

	if err := processUploadJob(ctx, d, "int-user", 1); err != nil {
		t.Fatalf("process: %v", err)
	}

	got, err = queue.GetJob(ctx, "int-user", 1)
	if err != nil {
		t.Fatalf("get job after process: %v", err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want done", got.Status)
	}
	if len(got.NoteLinks) != 2 {
		t.Errorf("noteLinks = %v, want 2 items", got.NoteLinks)
	}
	if len(nc.calls) != 2 {
		t.Errorf("note creator calls = %d, want 2", len(nc.calls))
	}
}

func TestIntegration_PublishToFailure(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newTestQueue(t)

	d := &mockDepsAll{
		transcriber: &stubTranscriber{err: ErrNotFound},
		roster:      &stubRoster{},
		uploadQueue: queue,
		uploadRepo:  &UploadRepo{db: nil},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{
		UserID: "int-user", UploadID: 1, FilePath: audioPath, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	err := processUploadJob(ctx, d, "int-user", 1)
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "int-user", 1)
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
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	uploadRepo := &UploadRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "int-user", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newTestQueue(t)
	failingTranscriber := &stubTranscriber{err: ErrNotFound}
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: failingTranscriber,
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date:     "2026-01-01",
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", Summary: "ok", Confidence: 0.9}},
		}},
		noteCreator: nc,
		uploadQueue: queue,
		studentRepo: studentRepo,
		uploadRepo:  uploadRepo,
	}

	old := serviceDeps
	serviceDeps = d
	t.Cleanup(func() { serviceDeps = old })

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{
		UserID: "int-user", UploadID: 1, FilePath: audioPath, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// First attempt fails.
	if err := processUploadJob(ctx, d, "int-user", 1); err == nil {
		t.Fatal("expected error on first attempt")
	}
	got, err := queue.GetJob(ctx, "int-user", 1)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}

	// Fix the transcriber.
	failingTranscriber.err = nil
	failingTranscriber.result = "transcript"

	// Retry via handler.
	req := httptest.NewRequest(http.MethodPost, "/jobs/retry", http.NoBody)
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "int-user"},
	})
	req = req.WithContext(rctx)
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
	if err := processUploadJob(ctx, d, "int-user", 1); err != nil {
		t.Fatalf("second process: %v", err)
	}

	got, err = queue.GetJob(ctx, "int-user", 1)
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
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	doneJob, err := queue.GetJob(ctx, "u1", 1)
	if err != nil {
		t.Fatal(err)
	}
	doneJob.Status = JobStatusDone
	doneJob.NoteLinks = []NoteLink{{Name: "Test Student", NoteID: 1, StudentID: 5, ClassName: "Math"}}
	if err := queue.UpdateJob(ctx, *doneJob); err != nil {
		t.Fatal(err)
	}

	// Job 2: failed.
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 2, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	failedJob, err := queue.GetJob(ctx, "u1", 2)
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
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 3, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	old := serviceDeps
	serviceDeps = &mockDepsAll{uploadQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody)
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "u1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp JobListResponse
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

func TestIntegration_UpdateReportExample(t *testing.T) {
	db := setupTestDB(t)
	exampleRepo := &ReportExampleRepo{db: db}

	// Create an example first
	ex, err := exampleRepo.Create(t.Context(), "user1", "original.txt", "original content")
	if err != nil {
		t.Fatal(err)
	}

	store := newDBExampleStore(exampleRepo)
	old := serviceDeps
	serviceDeps = &mockDepsAll{exampleStore: store}
	t.Cleanup(func() { serviceDeps = old })

	// Update via full Handle router
	body, err := json.Marshal(map[string]string{"name": "updated.txt", "content": "updated content"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/report-examples/%d", ex.ID), bytes.NewReader(body))
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result ReportExample
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "updated.txt" {
		t.Errorf("name = %q, want updated.txt", result.Name)
	}
}
