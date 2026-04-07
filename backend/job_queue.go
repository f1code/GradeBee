// job_queue.go defines the generic job queue interfaces used for async
// processing. The in-memory implementation lives in job_queue_mem.go.
package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// Keyed is the constraint for job types stored in a JobQueue.
// Each job must provide a unique key and an owner identifier for listing.
type Keyed interface {
	JobKey() string
	OwnerID() string
}

// JobQueue abstracts typed job queue operations.
type JobQueue[T Keyed] interface {
	// Publish stores the job and dispatches it for async processing.
	// Caller must set status/state before calling Publish — the queue
	// does not modify job fields. If status is not set, the processor's
	// idempotency check may silently skip the job.
	Publish(ctx context.Context, job T) error
	// GetJob reads a single job by key.
	GetJob(ctx context.Context, key string) (*T, error)
	// UpdateJob writes the full job state back.
	UpdateJob(ctx context.Context, job T) error
	// ListJobs returns all jobs for the given owner.
	ListJobs(ctx context.Context, ownerID string) ([]T, error)
	// DeleteJob removes a job from the store.
	DeleteJob(ctx context.Context, key string) error
	// Close tears down the queue and stops workers.
	Close()
}

// saveToUploadsDir writes data to the uploads directory with a unique filename
// built from a UUID and the given extension (e.g. ".pdf"). Returns the full
// disk path. Callers are responsible for cleanup on downstream failures.
func saveToUploadsDir(data []byte, ext string) (string, error) {
	uploadsDir := serviceDeps.GetUploadsDir()
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)
	if err := os.WriteFile(diskPath, data, 0o644); err != nil {
		return "", fmt.Errorf("save to uploads dir: %w", err)
	}
	return diskPath, nil
}

// publishOrCleanup publishes a job to the queue. If publishing fails (including
// queue unavailability), it runs all cleanup functions best-effort and returns
// the error.
func publishOrCleanup[T Keyed](ctx context.Context, queue JobQueue[T], job T, cleanups ...func()) error {
	if err := queue.Publish(ctx, job); err != nil {
		for _, fn := range cleanups {
			fn()
		}
		return err
	}
	return nil
}
