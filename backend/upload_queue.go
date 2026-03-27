// upload_queue.go defines the UploadQueue interface and the UploadJob type
// used for async upload processing. The in-memory implementation lives in
// mem_queue.go; tests use the stubUploadQueue in testutil_test.go.
package handler

import (
	"context"
	"fmt"
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

// NoteLink pairs a student name with the ID of the created note.
type NoteLink struct {
	Name   string `json:"name"`
	NoteID int64  `json:"noteId"`
}

// UploadJob represents an async upload processing job.
type UploadJob struct {
	UserID    string     `json:"userId"`
	UploadID  int64      `json:"uploadId"`
	FilePath  string     `json:"filePath"`
	FileName  string     `json:"fileName"`
	MimeType  string     `json:"mimeType"`
	Source    string     `json:"source"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	NoteLinks []NoteLink `json:"noteLinks,omitempty"`
	Error     string     `json:"error,omitempty"`
	FailedAt  *time.Time `json:"failedAt,omitempty"`
}

// kvKey returns the KV key for a job: "<userId>/<uploadId>".
func kvKey(userID string, uploadID int64) string {
	return fmt.Sprintf("%s/%d", userID, uploadID)
}

// UploadQueue abstracts job queue operations for upload processing.
type UploadQueue interface {
	// Publish writes the job with status "queued" and dispatches it for processing.
	Publish(ctx context.Context, job UploadJob) error
	// GetJob reads a single job.
	GetJob(ctx context.Context, userID string, uploadID int64) (*UploadJob, error)
	// UpdateJob writes the full job state back.
	UpdateJob(ctx context.Context, job UploadJob) error
	// ListJobs returns all jobs for the given user.
	ListJobs(ctx context.Context, userID string) ([]UploadJob, error)
	// DeleteJob removes a job from the store.
	DeleteJob(ctx context.Context, userID string, uploadID int64) error
	// Close tears down the queue and stops workers.
	Close()
}
