// reports_handler.go handles report generation, regeneration, listing,
// fetching, and deletion endpoints.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type generateReportsRequest struct {
	Students     []reportStudentInput `json:"students"`
	StartDate    string               `json:"startDate"`
	EndDate      string               `json:"endDate"`
	Instructions string               `json:"instructions"`
}

type reportStudentInput struct {
	StudentID int64  `json:"studentId"`
	Name      string `json:"name"`
	Class     string `json:"class"`
}

type reportResult struct {
	ID        int64  `json:"id"`
	StudentID int64  `json:"studentId"`
	Student   string `json:"student"`
	Class     string `json:"class"`
	HTML      string `json:"html"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	CreatedAt string `json:"createdAt"`
}

type generateReportsResponse struct {
	Reports []reportResult `json:"reports"`
	Error   *string        `json:"error"`
}

func handleGenerateReports(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req generateReportsRequest
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
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	generator, err := serviceDeps.GetReportGenerator()
	if err != nil {
		log.Error("generate reports: init failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var reports []reportResult

	for _, s := range req.Students {
		// Verify student ownership
		owns, err := serviceDeps.GetStudentRepo().BelongsToUser(ctx, s.StudentID, userID)
		if err != nil || !owns {
			errMsg := fmt.Sprintf("student %d not found", s.StudentID)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": errMsg})
			return
		}

		resp, err := generator.Generate(ctx, GenerateReportRequest{
			StudentID:    s.StudentID,
			Student:      s.Name,
			Class:        s.Class,
			StartDate:    req.StartDate,
			EndDate:      req.EndDate,
			UserID:       userID,
			Instructions: req.Instructions,
		})
		if err != nil {
			errMsg := fmt.Sprintf("failed to generate report for %s: %s", s.Name, err.Error())
			log.Error("generate reports: student failed", "student", s.Name, "error", err)
			writeJSON(w, http.StatusOK, generateReportsResponse{
				Reports: reports,
				Error:   &errMsg,
			})
			return
		}
		reports = append(reports, reportResult{
			ID:        resp.ReportID,
			StudentID: s.StudentID,
			Student:   s.Name,
			Class:     s.Class,
			HTML:      resp.HTML,
			StartDate: req.StartDate,
			EndDate:   req.EndDate,
			CreatedAt: resp.CreatedAt,
		})
	}

	log.Info("generate reports completed", "user_id", userID, "report_count", len(reports))
	writeJSON(w, http.StatusOK, generateReportsResponse{
		Reports: reports,
		Error:   nil,
	})
}

type regenerateReportRequest struct {
	Feedback string `json:"feedback"`
}

func handleRegenerateReport(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	// Extract report ID from URL path
	reportID, ok := pathParam(r.URL.Path, "/reports/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report id"})
		return
	}

	var req regenerateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Verify ownership
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(ctx, rpt.StudentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}

	// Load student + class from DB
	student, err := serviceDeps.GetStudentRepo().GetByID(ctx, rpt.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load student"})
		return
	}
	class, err := serviceDeps.GetClassRepo().GetByID(ctx, student.ClassID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load class"})
		return
	}

	generator, err := serviceDeps.GetReportGenerator()
	if err != nil {
		log.Error("regenerate report: init failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var instructions string
	if rpt.Instructions != nil {
		instructions = *rpt.Instructions
	}

	resp, err := generator.Regenerate(ctx, RegenerateReportRequest{
		ReportID:     rpt.ID,
		Feedback:     req.Feedback,
		StudentID:    rpt.StudentID,
		Student:      student.Name,
		Class:        class.Name,
		StartDate:    rpt.StartDate,
		EndDate:      rpt.EndDate,
		UserID:       userID,
		Instructions: instructions,
	})
	if err != nil {
		log.Error("regenerate report failed", "student", student.Name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	log.Info("regenerate report completed", "user_id", userID, "report_id", resp.ReportID)
	writeJSON(w, http.StatusOK, resp)
}

// --- Report CRUD handlers ---

func handleListReports(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	studentID, ok := pathParam(r.URL.Path, "/students/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), studentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	}
	reports, err := serviceDeps.GetReportRepo().List(r.Context(), studentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if reports == nil {
		reports = []Report{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports})
}

func handleGetReport(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	reportID, ok := pathParam(r.URL.Path, "/reports/")
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), rpt.StudentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}
	writeJSON(w, http.StatusOK, rpt)
}

func handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	reportID, ok := pathParam(r.URL.Path, "/reports/")
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), rpt.StudentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}
	if err := serviceDeps.GetReportRepo().Delete(r.Context(), reportID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
