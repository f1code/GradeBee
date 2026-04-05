// job_queue.go defines the generic job queue interfaces used for async
// processing. The in-memory implementation lives in job_queue_mem.go.
package handler

import "context"

// Keyed is the constraint for job types stored in a JobQueue.
// Each job must provide a unique key and an owner identifier for listing.
type Keyed interface {
	JobKey() string
	OwnerID() string
}

// JobQueue abstracts typed job queue operations.
type JobQueue[T Keyed] interface {
	// Publish stores the job and dispatches it for async processing.
	// Caller must set status/state before calling Publish.
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
