// report_batch.go defines the report batch shapes sent to the page: rows,
// statuses and the batch snapshot.
package handler

import "time"

// Report batch row statuses.
const (
	ReportStudentQueued     = "queued"
	ReportStudentGenerating = "generating"
	ReportStudentDone       = "done"
	ReportStudentFailed     = "failed"
)

// Report batch statuses, derived from the rows.
const (
	ReportBatchRunning  = "running"
	ReportBatchFinished = "finished"
)

// maxConsecutiveReportFailures stops a batch whose failures look systemic
// (provider down, quota) instead of burning one LLM call per student.
const maxConsecutiveReportFailures = 3

// Row error texts. Fixed strings: provider errors can echo prompt content,
// and rows must not carry student names (ADR 0003).
const (
	reportStudentErrGenerate = "report generation failed"
	reportStudentErrSkipped  = "skipped after repeated errors"
)

// ReportBatchStudent is one student's outcome in a report batch.
type ReportBatchStudent struct {
	StudentID int64   `json:"studentId"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	ReportID  *int64  `json:"reportId,omitempty"`
	Error     *string `json:"error,omitempty"`
}

// ReportBatch is the wire view of a ReportJob.
type ReportBatch struct {
	ID        string               `json:"id"`
	StartDate string               `json:"startDate"`
	EndDate   string               `json:"endDate"`
	CreatedAt time.Time            `json:"createdAt"`
	Status    string               `json:"status"`
	Students  []ReportBatchStudent `json:"students"`
}
