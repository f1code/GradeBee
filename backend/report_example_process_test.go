package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubExtractionQueue implements JobQueue[ExtractionJob] for tests.
type stubExtractionQueue struct {
	jobs      map[string]ExtractionJob
	published []ExtractionJob
}

func newStubExtractionQueue() *stubExtractionQueue {
	return &stubExtractionQueue{jobs: make(map[string]ExtractionJob)}
}

func (q *stubExtractionQueue) Publish(_ context.Context, job ExtractionJob) error {
	q.jobs[job.JobKey()] = job
	q.published = append(q.published, job)
	return nil
}

func (q *stubExtractionQueue) GetJob(_ context.Context, key string) (*ExtractionJob, error) {
	job, ok := q.jobs[key]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", key)
	}
	return &job, nil
}

func (q *stubExtractionQueue) UpdateJob(_ context.Context, job ExtractionJob) error {
	q.jobs[job.JobKey()] = job
	return nil
}

func (q *stubExtractionQueue) ListJobs(_ context.Context, ownerID string) ([]ExtractionJob, error) {
	prefix := ownerID + "/"
	var jobs []ExtractionJob
	for k, j := range q.jobs {
		if strings.HasPrefix(k, prefix) {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *stubExtractionQueue) DeleteJob(_ context.Context, key string) error {
	delete(q.jobs, key)
	return nil
}

func (q *stubExtractionQueue) Close() {}

func TestProcessExtraction_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "report.png")
	if err := os.WriteFile(filePath, []byte("fake image data"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newStubExtractionQueue()
	exStore := &stubExampleStore{}
	exExtractor := &stubExampleExtractor{result: "Extracted report card text"}
	d := &mockDepsAll{
		exampleStore:     exStore,
		exampleExtractor: exExtractor,
	}

	ctx := context.Background()
	job := ExtractionJob{
		UserID:    "user1",
		ExampleID: 42,
		FilePath:  filePath,
		FileName:  "report.png",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}

	key := job.JobKey()
	if err := processExtraction(ctx, d, queue, key); err != nil {
		t.Fatalf("processExtraction: %v", err)
	}

	got, err := queue.GetJob(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want %q", got.Status, JobStatusDone)
	}

	// Verify example was updated.
	if len(exStore.updateStatusCalls) != 1 {
		t.Fatalf("updateStatusCalls = %d, want 1", len(exStore.updateStatusCalls))
	}
	call := exStore.updateStatusCalls[0]
	if call.ID != 42 || call.Status != "ready" || call.Content != "Extracted report card text" {
		t.Errorf("updateStatus call = %+v", call)
	}

	// Verify file was cleaned up.
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("expected file to be deleted after extraction")
	}
}

func TestProcessExtraction_ExtractFails(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "report.pdf")
	if err := os.WriteFile(filePath, []byte("fake pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newStubExtractionQueue()
	exStore := &stubExampleStore{}
	exExtractor := &stubExampleExtractor{err: fmt.Errorf("GPT error")}
	d := &mockDepsAll{
		exampleStore:     exStore,
		exampleExtractor: exExtractor,
	}

	ctx := context.Background()
	job := ExtractionJob{
		UserID:    "user1",
		ExampleID: 10,
		FilePath:  filePath,
		FileName:  "report.pdf",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}

	err := processExtraction(ctx, d, queue, job.JobKey())
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, job.JobKey())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
	if !strings.Contains(got.Error, "extract") {
		t.Errorf("error = %q, want to contain 'extract'", got.Error)
	}

	// Verify example was marked failed.
	if len(exStore.updateStatusCalls) != 1 {
		t.Fatalf("updateStatusCalls = %d, want 1", len(exStore.updateStatusCalls))
	}
	if exStore.updateStatusCalls[0].Status != "failed" {
		t.Errorf("example status = %q, want 'failed'", exStore.updateStatusCalls[0].Status)
	}
}

func TestProcessExtraction_FileNotFound(t *testing.T) {
	queue := newStubExtractionQueue()
	exStore := &stubExampleStore{}
	d := &mockDepsAll{exampleStore: exStore}

	ctx := context.Background()
	job := ExtractionJob{
		UserID:    "user1",
		ExampleID: 5,
		FilePath:  "/nonexistent/file.png",
		FileName:  "file.png",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}

	err := processExtraction(ctx, d, queue, job.JobKey())
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	got, gErr := queue.GetJob(ctx, job.JobKey())
	if gErr != nil {
		t.Fatal(gErr)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
}

func TestProcessExtraction_AlreadyProcessed(t *testing.T) {
	queue := newStubExtractionQueue()
	d := &mockDepsAll{}

	ctx := context.Background()
	key := "user1/ex-1"
	queue.jobs[key] = ExtractionJob{
		UserID: "user1", ExampleID: 1, Status: JobStatusDone,
	}

	err := processExtraction(ctx, d, queue, key)
	if err != nil {
		t.Fatalf("expected no error for already-processed job, got: %v", err)
	}

	got, gErr := queue.GetJob(ctx, key)
	if gErr != nil {
		t.Fatal(gErr)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status changed to %q, should remain done", got.Status)
	}
}
