// reports_handler.go handles report generation, batch listing and retry,
// regeneration, listing, fetching, and deletion endpoints.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ListReportsResponse is the JSON envelope for handleListReports.
type ListReportsResponse struct {
	Reports []ReportSummary `json:"reports"`
}

// GenerateReportsHTTPRequest is the JSON body for POST /reports.
type GenerateReportsHTTPRequest struct {
	Students     []ReportStudentInput `json:"students"`
	StartDate    string               `json:"startDate"`
	EndDate      string               `json:"endDate"`
	Instructions string               `json:"instructions"`
}

// ReportStudentInput identifies a student in a generate-reports request.
type ReportStudentInput struct {
	StudentID int64  `json:"studentId"`
	Name      string `json:"name"`
	ClassName string `json:"className"`
}

// ReportResult is the per-student result in a generate/regenerate response.
type ReportResult struct {
	ID        int64  `json:"id"`
	StudentID int64  `json:"studentId"`
	Student   string `json:"student"`
	ClassName string `json:"className"`
	HTML      string `json:"html"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	CreatedAt string `json:"createdAt"`
}

// ListReportBatchesResponse is the JSON response for GET /reports/batches,
// newest first.
type ListReportBatchesResponse struct {
	Batches []ReportBatch `json:"batches"`
}

const (
	errReportBatchRunning = "a report batch is already running"
	errReportBatchBusy    = "report generation is busy, try again shortly"
)

// takeReportBatchLock claims the Teacher's batch lock, or answers 409. The lock
// covers check, publish and prune only; the worker runs unlocked.
func takeReportBatchLock(w http.ResponseWriter, userID string) (release func(), ok bool) {
	key := "report-batch/" + userID
	if !takeUploadLock(key) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": errReportBatchRunning})
		return nil, false
	}
	return func() { releaseUploadLock(key) }, true
}

func writeReportBatchRunning(w http.ResponseWriter, batchID string) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": errReportBatchRunning, "batchId": batchID})
}

func handleGenerateReports(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req GenerateReportsHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(req.Students) == 0 || req.StartDate == "" || req.EndDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required fields (students, startDate, endDate)"})
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	ctx := r.Context()
	groupID, err := groupIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	// Verify ownership and resolve every student's inputs up front, so the
	// gate refuses the whole batch before any report is written.
	resolver := serviceDeps.GetReportInputResolver()
	inputs := make([]ReportInputs, 0, len(req.Students))
	seenLevels := map[string]bool{}
	var offendingLevels []string
	for _, s := range req.Students {
		// Name the student the caller asked about rather than echoing a bare row id
		// at them. It is their own input coming back, so it discloses nothing — and
		// requireStudentOwnership keeps it out of the log.
		who := strconv.FormatInt(s.StudentID, 10)
		if s.Name != "" {
			who = s.Name
		}
		if !requireStudentOwnership(w, r, s.StudentID, userID, fmt.Sprintf("student %s not found", who)) {
			return
		}
		in, err := resolver.ForGenerate(ctx, groupID, s, req.StartDate, req.EndDate, req.Instructions)
		var missing *ErrLevelInstructionsMissing
		if errors.As(err, &missing) {
			if !seenLevels[missing.LevelName] {
				seenLevels[missing.LevelName] = true
				offendingLevels = append(offendingLevels, missing.LevelName)
			}
			continue
		}
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		inputs = append(inputs, in)
	}

	if len(offendingLevels) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": levelsMissingInstructionsError(offendingLevels)})
		return
	}

	queue, err := serviceDeps.GetReportBatchQueue()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	release, ok := takeReportBatchLock(w, userID)
	if !ok {
		return
	}
	defer release()

	previous, err := queue.ListJobs(ctx, userID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	for _, j := range previous {
		if j.running() {
			writeReportBatchRunning(w, j.BatchID)
			return
		}
	}

	students := make([]ReportBatchStudent, len(inputs))
	for i, in := range inputs {
		students[i] = ReportBatchStudent{StudentID: in.StudentID, Name: in.StudentName, Status: ReportStudentQueued}
	}
	job := ReportJob{
		UserID:    userID,
		BatchID:   uuid.NewString(),
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		CreatedAt: time.Now().UTC(),
		Inputs:    inputs,
		Students:  students,
	}
	// Snapshot before publishing: once queued, the worker owns the job.
	snapshot := job.snapshot()
	// Publish stores the job before its channel send; a full channel leaves
	// a batch no worker will run, which would block every later POST.
	deleteJob := func(key string) {
		if err := queue.DeleteJob(ctx, key); err != nil {
			log.Warn("generate reports: delete batch failed", "error", err)
		}
	}
	if err := publishOrCleanup(ctx, queue, job, func() { deleteJob(job.JobKey()) }); err != nil {
		log.Error("generate reports: enqueue failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errReportBatchBusy})
		return
	}
	// Prune after publishing, so a 503 keeps the previous results.
	for _, j := range previous {
		deleteJob(j.JobKey())
	}

	log.Info("generate reports queued", "user_id", userID, "batch_id", job.BatchID, "student_count", len(students))
	writeJSON(w, http.StatusAccepted, snapshot)
}

func handleListReportBatches(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	queue, err := serviceDeps.GetReportBatchQueue()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	jobs, err := queue.ListJobs(r.Context(), userID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	slices.SortFunc(jobs, func(a, b ReportJob) int { return b.CreatedAt.Compare(a.CreatedAt) })
	batches := make([]ReportBatch, len(jobs))
	for i, j := range jobs {
		batches[i] = j.snapshot()
	}
	writeJSON(w, http.StatusOK, ListReportBatchesResponse{Batches: batches})
}

// Done rows stay done, so a retry gives no student a second report.
func handleRetryReportBatch(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	queue, err := serviceDeps.GetReportBatchQueue()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	ctx := r.Context()

	release, ok := takeReportBatchLock(w, userID)
	if !ok {
		return
	}
	defer release()

	// The caller's own key prefix: another Teacher's batch id is not found.
	job, err := queue.GetJob(ctx, reportBatchKey(userID, r.PathValue("id")))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report batch not found"})
		return
	}
	if job.running() {
		writeReportBatchRunning(w, job.BatchID)
		return
	}

	retried, requeued := job.requeueFailed()
	if requeued == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no failed students to retry"})
		return
	}

	snapshot := retried.snapshot()
	// Publish stores the queued rows before its channel send; put the failed
	// rows back, or the batch shows running forever.
	restore := func() {
		if err := queue.UpdateJob(ctx, *job); err != nil {
			log.Warn("retry report batch: restore failed", "error", err)
		}
	}
	if err := publishOrCleanup(ctx, queue, retried, restore); err != nil {
		log.Error("retry report batch: enqueue failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errReportBatchBusy})
		return
	}

	log.Info("report batch retry queued", "user_id", userID, "batch_id", job.BatchID, "student_count", requeued)
	writeJSON(w, http.StatusAccepted, snapshot)
}

// RegenerateReportHTTPRequest is the JSON body for POST /reports/:id/regenerate.
type RegenerateReportHTTPRequest struct {
	Feedback string `json:"feedback"`
}

func handleRegenerateReport(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	// Extract report ID from URL path
	reportID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report id"})
		return
	}

	var req RegenerateReportHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// The draft goes in the prompt: with no feedback the model returns it unchanged.
	if strings.TrimSpace(req.Feedback) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feedback is required"})
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	ctx := r.Context()

	// Load report from DB
	rpt, err := serviceDeps.GetReportRepo().GetByID(ctx, reportID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
			return
		}
		writeInternalError(w, r, err)
		return
	}

	if !requireStudentOwnership(w, r, rpt.StudentID, userID, "report not found") {
		return
	}

	groupID, err := groupIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	in, _, err := serviceDeps.GetReportInputResolver().ForReport(ctx, groupID, rpt.StudentID, rpt.ID)
	var missing *ErrLevelInstructionsMissing
	if errors.As(err, &missing) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": missing.Error()})
		return
	}
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	earlier, err := serviceDeps.GetFeedbackRepo().RegenerateChain(ctx, rpt.ID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	generator, err := serviceDeps.GetReportGenerator()
	if err != nil {
		log.Error("regenerate report: init failed", "error", err)
		writeInternalError(w, r, err)
		return
	}

	// Detached: a Teacher closing the tab still gets the report in History.
	// llmChatTimeout bounds the call.
	ctx = context.WithoutCancel(ctx)
	resp, err := generator.Regenerate(ctx, RegenerateReportRequest{
		ReportInputs: in,
		ReportRevision: ReportRevision{
			Draft:           rpt.HTML,
			DraftCreatedAt:  rpt.CreatedAt,
			EarlierFeedback: earlier,
			Feedback:        req.Feedback,
		},
		ReportID: rpt.ID,
		UserID:   userID,
	})
	if err != nil {
		log.Error("regenerate report failed", "student_id", rpt.StudentID, "error", err)
		writeInternalError(w, r, err)
		return
	}

	log.Info("regenerate report completed", "user_id", userID, "report_id", resp.ReportID)

	writeJSON(w, http.StatusOK, ReportResult{
		ID:        resp.ReportID,
		StudentID: in.StudentID,
		Student:   in.StudentName,
		ClassName: in.ClassName,
		HTML:      resp.HTML,
		StartDate: in.StartDate,
		EndDate:   in.EndDate,
		CreatedAt: resp.CreatedAt,
	})
}

// --- Report CRUD handlers ---

func handleListReports(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	studentID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
		return
	}
	reports, err := serviceDeps.GetReportRepo().List(r.Context(), studentID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if reports == nil {
		reports = []ReportSummary{}
	}
	writeJSON(w, http.StatusOK, ListReportsResponse{Reports: reports})
}

// ReportDetail is the response for GET /reports/:id — includes student/class names.
type ReportDetail struct {
	ReportResult `tstype:",extends"`
	Instructions *string `json:"instructions,omitempty"`
}

func handleGetReport(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	reportID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report id"})
		return
	}
	rpt, err := serviceDeps.GetReportRepo().GetByID(r.Context(), reportID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	if !requireStudentOwnership(w, r, rpt.StudentID, userID, "report not found") {
		return
	}
	student, err := serviceDeps.GetStudentRepo().GetByID(r.Context(), rpt.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load student"})
		return
	}
	cls, err := serviceDeps.GetClassRepo().GetByID(r.Context(), student.ClassID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load class"})
		return
	}
	writeJSON(w, http.StatusOK, ReportDetail{
		ReportResult: ReportResult{
			ID:        rpt.ID,
			StudentID: rpt.StudentID,
			Student:   student.Name,
			ClassName: cls.Name,
			HTML:      rpt.HTML,
			StartDate: rpt.StartDate,
			EndDate:   rpt.EndDate,
			CreatedAt: rpt.CreatedAt,
		},
		Instructions: rpt.Instructions,
	})
}

func handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	reportID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report id"})
		return
	}
	rpt, err := serviceDeps.GetReportRepo().GetByID(r.Context(), reportID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	if !requireStudentOwnership(w, r, rpt.StudentID, userID, "report not found") {
		return
	}
	if err := serviceDeps.GetReportRepo().Delete(r.Context(), reportID); err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
