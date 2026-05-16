package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.WriteFile(filePath, []byte("fake image data"), 0o644))

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
	require.NoError(t, queue.Publish(ctx, job))

	key := job.JobKey()
	require.NoError(t, processExtraction(ctx, d, queue, key))

	got, err := queue.GetJob(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)

	// Verify example was updated.
	require.Len(t, exStore.updateStatusCalls, 1)
	call := exStore.updateStatusCalls[0]
	assert.Equal(t, int64(42), call.ID)
	assert.Equal(t, "ready", call.Status)
	assert.Equal(t, "Extracted report card text", call.Content)

	// Verify file was cleaned up.
	_, err = os.Stat(filePath)
	assert.True(t, os.IsNotExist(err), "expected file to be deleted after extraction")
}

func TestProcessExtraction_ExtractFails(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "report.pdf")
	require.NoError(t, os.WriteFile(filePath, []byte("fake pdf"), 0o644))

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
	require.NoError(t, queue.Publish(ctx, job))

	err := processExtraction(ctx, d, queue, job.JobKey())
	require.Error(t, err)

	got, err := queue.GetJob(ctx, job.JobKey())
	require.NoError(t, err)
	assert.Equal(t, JobStatusFailed, got.Status)
	assert.Contains(t, got.Error, "extract")

	// Verify example was marked failed.
	require.Len(t, exStore.updateStatusCalls, 1)
	assert.Equal(t, "failed", exStore.updateStatusCalls[0].Status)
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
	require.NoError(t, queue.Publish(ctx, job))

	err := processExtraction(ctx, d, queue, job.JobKey())
	require.Error(t, err, "expected error for missing file")

	got, gErr := queue.GetJob(ctx, job.JobKey())
	require.NoError(t, gErr)
	assert.Equal(t, JobStatusFailed, got.Status)
}

func TestProcessExtraction_AlreadyProcessed(t *testing.T) {
	queue := newStubExtractionQueue()
	d := &mockDepsAll{}

	ctx := context.Background()
	key := "user1/ex-1"
	queue.jobs[key] = ExtractionJob{
		UserID: "user1", ExampleID: 1, Status: JobStatusDone,
	}

	require.NoError(t, processExtraction(ctx, d, queue, key), "expected no error for already-processed job")

	got, gErr := queue.GetJob(ctx, key)
	require.NoError(t, gErr)
	assert.Equal(t, JobStatusDone, got.Status, "status should remain done")
}
