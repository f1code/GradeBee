// report_example_job.go defines the ExtractionJob type for async report
// example text extraction (PDF/image → GPT Vision → stored text).
package handler

import (
	"fmt"
	"time"
)

// ExtractionJob represents an async report example text extraction job.
type ExtractionJob struct {
	UserID    string    `json:"userId"`
	ExampleID int64     `json:"exampleId"`
	FilePath  string    `json:"filePath"`
	FileName  string    `json:"fileName"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// JobKey implements Keyed.
func (j ExtractionJob) JobKey() string {
	return fmt.Sprintf("%s/ex-%d", j.UserID, j.ExampleID)
}

// OwnerID implements Keyed.
func (j ExtractionJob) OwnerID() string { return j.UserID }
