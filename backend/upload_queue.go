// upload_queue.go defines the UploadQueue interface and the UploadJob type
// used for async upload processing. The in-memory implementation lives in
// mem_queue.go; tests use the stubUploadQueue in testutil_test.go.
package handler

import (
	"context"
	"time"
)

// Job status constants.
const (
	JobStatusQueued        = "queued"
	JobStatusTranscribing  = "transcribing"
	JobStatusExtracting    = "extracting"
	JobStatusCreatingNotes = "creating_notes"
	JobStatusDone          = "done"
	JobStatusFailed        = "failed"
)

// UploadJob represents an async upload processing job.
type UploadJob struct {
	UserID    string     `json:"userId"`
	FileID    string     `json:"fileId"`
	FileName  string     `json:"fileName"`
	MimeType  string     `json:"mimeType"`
	Source    string     `json:"source"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	NoteIDs   []string   `json:"noteIds,omitempty"`
	Error     string     `json:"error,omitempty"`
	FailedAt  *time.Time `json:"failedAt,omitempty"`
}

// kvKey returns the KV key for a job: "<userId>/<fileId>".
func kvKey(userID, fileID string) string {
	return userID + "/" + fileID
}

// UploadQueue abstracts job queue operations for upload processing.
type UploadQueue interface {
	// Publish writes the job with status "queued" and dispatches it for processing.
	Publish(ctx context.Context, job UploadJob) error
	// GetJob reads a single job.
	GetJob(ctx context.Context, userID, fileID string) (*UploadJob, error)
	// UpdateJob writes the full job state back.
	UpdateJob(ctx context.Context, job UploadJob) error
	// ListJobs returns all jobs for the given user.
	ListJobs(ctx context.Context, userID string) ([]UploadJob, error)
	// Close tears down the queue and stops workers.
	Close()
}
