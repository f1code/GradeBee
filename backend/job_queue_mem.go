// job_queue_mem.go provides a generic in-memory JobQueue implementation
// backed by a map and a buffered channel with a worker pool.
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// ProcessFunc is called by queue workers to process a job identified by key.
// It receives the queue (for reading/updating job state) and the job key.
type ProcessFunc[T Keyed] func(ctx context.Context, q JobQueue[T], key string) error

// MemQueue is a generic in-memory job queue with a background worker pool.
type MemQueue[T Keyed] struct {
	mu      sync.RWMutex
	jobs    map[string]T
	work    chan string // job keys
	process ProcessFunc[T]
	cancel  context.CancelFunc
}

// NewMemQueue creates a MemQueue and starts worker goroutines.
// Pass a non-zero workers count (e.g. 4). The process function is called
// by workers for each dispatched job.
func NewMemQueue[T Keyed](process ProcessFunc[T], workers int) *MemQueue[T] {
	ctx, cancel := context.WithCancel(context.Background())
	q := &MemQueue[T]{
		jobs:    make(map[string]T),
		work:    make(chan string, 100),
		process: process,
		cancel:  cancel,
	}
	for i := 0; i < workers; i++ {
		go q.worker(ctx)
	}
	return q
}

func (q *MemQueue[T]) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-q.work:
			if ctx.Err() != nil {
				return
			}
			if err := q.process(ctx, q, key); err != nil {
				slog.Error("MemQueue worker: job failed", "key", key, "error", err)
			}
		}
	}
}

func (q *MemQueue[T]) Publish(_ context.Context, job T) error {
	key := job.JobKey()
	q.mu.Lock()
	q.jobs[key] = job
	q.mu.Unlock()

	select {
	case q.work <- key:
	default:
		return fmt.Errorf("MemQueue: work channel full, job %s dropped", key)
	}
	return nil
}

func (q *MemQueue[T]) GetJob(_ context.Context, key string) (*T, error) {
	q.mu.RLock()
	job, ok := q.jobs[key]
	q.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("job not found: %s", key)
	}
	return &job, nil
}

func (q *MemQueue[T]) UpdateJob(_ context.Context, job T) error {
	key := job.JobKey()
	q.mu.Lock()
	q.jobs[key] = job
	q.mu.Unlock()
	return nil
}

func (q *MemQueue[T]) ListJobs(_ context.Context, ownerID string) ([]T, error) {
	prefix := ownerID + "/"
	q.mu.RLock()
	defer q.mu.RUnlock()

	var jobs []T
	for k, j := range q.jobs {
		if strings.HasPrefix(k, prefix) {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *MemQueue[T]) DeleteJob(_ context.Context, key string) error {
	q.mu.Lock()
	delete(q.jobs, key)
	q.mu.Unlock()
	return nil
}

func (q *MemQueue[T]) Close() {
	q.cancel()
}
