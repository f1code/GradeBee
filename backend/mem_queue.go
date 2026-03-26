// mem_queue.go provides an in-memory UploadQueue implementation backed by a
// map and a buffered channel. Worker goroutines pull job references from the
// channel and call processUploadJob. Used in both production (single-binary
// deployment) and tests.
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// memQueue is an in-memory UploadQueue with a background worker pool.
type memQueue struct {
	mu     sync.RWMutex
	jobs   map[string]UploadJob // keyed by "userId/fileId"
	work   chan jobRef          // buffered channel for pending work
	d      deps                 // deps for processUploadJob calls
	cancel context.CancelFunc   // cancels worker goroutines
}

// jobRef identifies a job for the worker channel.
type jobRef struct {
	UserID string
	FileID string
}

// NewMemQueue creates an in-memory upload queue and starts worker goroutines.
// The workers call processUploadJob using the provided deps. Pass a non-zero
// workers count (e.g. 4).
func NewMemQueue(d deps, workers int) *memQueue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &memQueue{
		jobs:   make(map[string]UploadJob),
		work:   make(chan jobRef, 100),
		d:      d,
		cancel: cancel,
	}
	for i := 0; i < workers; i++ {
		go q.worker(ctx)
	}
	return q
}

func (q *memQueue) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ref := <-q.work:
			if ctx.Err() != nil {
				return
			}
			if err := processUploadJob(ctx, q.d, ref.UserID, ref.FileID); err != nil {
				slog.Error("memQueue worker: job failed", "user_id", ref.UserID, "file_id", ref.FileID, "error", err)
			}
		}
	}
}

func (q *memQueue) Publish(_ context.Context, job UploadJob) error {
	job.Status = JobStatusQueued

	key := kvKey(job.UserID, job.FileID)
	q.mu.Lock()
	q.jobs[key] = job
	q.mu.Unlock()

	select {
	case q.work <- jobRef{UserID: job.UserID, FileID: job.FileID}:
	default:
		return fmt.Errorf("memQueue: work channel full, job %s/%s dropped", job.UserID, job.FileID)
	}
	return nil
}

func (q *memQueue) GetJob(_ context.Context, userID, fileID string) (*UploadJob, error) {
	key := kvKey(userID, fileID)
	q.mu.RLock()
	job, ok := q.jobs[key]
	q.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("job not found: %s/%s", userID, fileID)
	}
	return &job, nil
}

func (q *memQueue) UpdateJob(_ context.Context, job UploadJob) error {
	key := kvKey(job.UserID, job.FileID)
	q.mu.Lock()
	q.jobs[key] = job
	q.mu.Unlock()
	return nil
}

func (q *memQueue) ListJobs(_ context.Context, userID string) ([]UploadJob, error) {
	prefix := userID + "/"
	q.mu.RLock()
	defer q.mu.RUnlock()

	var jobs []UploadJob
	for k, j := range q.jobs {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *memQueue) DeleteJob(_ context.Context, userID, fileID string) error {
	key := kvKey(userID, fileID)
	q.mu.Lock()
	delete(q.jobs, key)
	q.mu.Unlock()
	return nil
}

func (q *memQueue) Close() {
	q.cancel()
}
