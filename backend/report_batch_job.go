// report_batch_job.go defines the report batch job and the worker that runs
// it. POST /reports enqueues one ReportJob per batch on a 1-worker MemQueue;
// the page polls GET /reports/batches for per-student progress. Excluded from
// tygo: the job carries server-side inputs.
package handler

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

// ReportJob is a batch in the queue. Inputs carry notes and instructions, so
// it never goes on the wire: handlers answer with snapshot().
type ReportJob struct {
	UserID    string
	BatchID   string
	StartDate string
	EndDate   string
	CreatedAt time.Time
	// Inputs[i] is Students[i]'s, resolved at POST time.
	Inputs   []ReportInputs
	Students []ReportBatchStudent
}

func reportBatchKey(userID, batchID string) string { return userID + "/" + batchID }

// JobKey implements Keyed.
func (j ReportJob) JobKey() string { return reportBatchKey(j.UserID, j.BatchID) }

// OwnerID implements Keyed.
func (j ReportJob) OwnerID() string { return j.UserID }

func (j ReportJob) running() bool {
	return slices.ContainsFunc(j.Students, func(s ReportBatchStudent) bool {
		return s.Status == ReportStudentQueued || s.Status == ReportStudentGenerating
	})
}

func (j ReportJob) snapshot() ReportBatch {
	status := ReportBatchFinished
	if j.running() {
		status = ReportBatchRunning
	}
	return ReportBatch{
		ID:        j.BatchID,
		StartDate: j.StartDate,
		EndDate:   j.EndDate,
		CreatedAt: j.CreatedAt,
		Status:    status,
		Students:  j.Students,
	}
}

// requeueFailed returns the job with its failed rows queued again, and how
// many. It edits a clone: the receiver's rows are shared (see updateReportRows).
func (j ReportJob) requeueFailed() (requeued ReportJob, n int) {
	j.Students = slices.Clone(j.Students)
	for i := range j.Students {
		if j.Students[i].Status == ReportStudentFailed {
			j.Students[i].Status = ReportStudentQueued
			j.Students[i].Error = nil
			n++
		}
	}
	return j, n
}

// updateReportRows re-reads the job and writes edit's changes on a clone of
// its rows. GetJob copies share the Students array with the stored job and
// with whatever a handler is serialising, so rows are never edited in place.
func updateReportRows(ctx context.Context, q JobQueue[ReportJob], key string, edit func([]ReportBatchStudent)) error {
	job, err := q.GetJob(ctx, key)
	if err != nil {
		return err
	}
	job.Students = slices.Clone(job.Students)
	edit(job.Students)
	return q.UpdateJob(ctx, *job)
}

func failRemaining(rows []ReportBatchStudent, msg string) {
	for i := range rows {
		if rows[i].Status == ReportStudentQueued {
			rows[i].Status = ReportStudentFailed
			rows[i].Error = new(msg)
		}
	}
}

// processReportBatch is the ProcessFunc for the report batch queue. It
// generates one report per queued row, in order. The batch's last row write
// is its last write: a later one would re-insert a batch the next POST pruned.
func processReportBatch(ctx context.Context, d deps, q JobQueue[ReportJob], key string) error {
	job, err := q.GetJob(ctx, key)
	if err != nil {
		return err
	}
	ctx = withLLMCaller(ctx, job.UserID, "")
	log := slog.With("user_id", job.UserID, "batch_id", job.BatchID)

	generator, err := d.GetReportGenerator()
	if err != nil {
		log.Error("report batch: init failed", "error", err)
		return updateReportRows(ctx, q, key, func(rows []ReportBatchStudent) {
			failRemaining(rows, reportStudentErrGenerate)
		})
	}

	consecutiveFailures := 0
	generated := 0
	for i, in := range job.Inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Only this worker writes rows while the batch runs, so the first read
		// holds. A retried batch keeps its done rows.
		if job.Students[i].Status != ReportStudentQueued {
			continue
		}
		if err := updateReportRows(ctx, q, key, func(rows []ReportBatchStudent) {
			rows[i].Status = ReportStudentGenerating
		}); err != nil {
			return err
		}

		resp, genErr := generator.Generate(ctx, GenerateReportRequest{ReportInputs: in, UserID: job.UserID})
		if genErr != nil {
			log.Error("report batch: student failed", "student_id", in.StudentID, "error", genErr)
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
			generated++
		}
		if err := updateReportRows(ctx, q, key, func(rows []ReportBatchStudent) {
			if genErr != nil {
				rows[i].Status = ReportStudentFailed
				rows[i].Error = new(reportStudentErrGenerate)
			} else {
				rows[i].Status = ReportStudentDone
				rows[i].ReportID = &resp.ReportID
			}
			if consecutiveFailures >= maxConsecutiveReportFailures {
				failRemaining(rows, reportStudentErrSkipped)
			}
		}); err != nil {
			return err
		}
		if consecutiveFailures >= maxConsecutiveReportFailures {
			break
		}
	}

	log.Info("report batch completed", "report_count", generated, "student_count", len(job.Students))
	return nil
}
