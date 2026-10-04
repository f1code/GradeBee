// reports_handler.go handles report generation, regeneration, listing,
// fetching, and deletion endpoints.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

// GenerateReportsHTTPResponse is the JSON response for POST /reports.
type GenerateReportsHTTPResponse struct {
	Reports []ReportResult `json:"reports"`
	Error   *string        `json:"error"`
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
	generator, err := serviceDeps.GetReportGenerator()
	if err != nil {
		log.Error("generate reports: init failed", "error", err)
		writeInternalError(w, r, err)
		return
	}

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

	reports := []ReportResult{}

	for _, in := range inputs {
		resp, err := generator.Generate(ctx, GenerateReportRequest{ReportInputs: in, UserID: userID})
		if err != nil {
			errMsg := fmt.Sprintf("failed to generate report for %s", in.StudentName)
			log.Error("generate reports: student failed", "student_id", in.StudentID, "error", err)
			writeJSON(w, http.StatusOK, GenerateReportsHTTPResponse{
				Reports: reports,
				Error:   &errMsg,
			})
			return
		}
		reports = append(reports, ReportResult{
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

	log.Info("generate reports completed", "user_id", userID, "report_count", len(reports))
	writeJSON(w, http.StatusOK, GenerateReportsHTTPResponse{
		Reports: reports,
		Error:   nil,
	})
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

	generator, err := serviceDeps.GetReportGenerator()
	if err != nil {
		log.Error("regenerate report: init failed", "error", err)
		writeInternalError(w, r, err)
		return
	}

	resp, err := generator.Regenerate(ctx, RegenerateReportRequest{
		ReportInputs: in,
		ReportID:     rpt.ID,
		Feedback:     req.Feedback,
		UserID:       userID,
	})
	if err != nil {
		log.Error("regenerate report failed", "student_id", rpt.StudentID, "error", err)
		writeInternalError(w, r, err)
		return
	}

	log.Info("regenerate report completed", "user_id", userID, "report_id", resp.ReportID)

	// Implicit signal: regenerating always records a thumbs-down on the *original* report.
	// Best-effort: a failure here should not fail the regen response.
	if feedbackRepo := serviceDeps.GetFeedbackRepo(); feedbackRepo != nil {
		var feedbackComment *string
		if req.Feedback != "" {
			feedbackComment = &req.Feedback
		}
		if _, fbErr := feedbackRepo.Insert(ctx, ArtifactFeedback{
			ArtifactType: "report",
			ArtifactID:   rpt.ID, // original report, not the new one
			Rating:       "down",
			Signal:       "regenerated",
			Comment:      feedbackComment,
			UserID:       userID,
		}); fbErr != nil {
			log.Warn("implicit regen feedback insert failed", "error", fbErr)
		}
	}

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
