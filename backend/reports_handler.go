// reports_handler.go handles the POST /reports and POST /reports/regenerate endpoints.
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
	Name  string `json:"name"`
	Class string `json:"class"`
}

type reportResult struct {
	Student string `json:"student"`
	Class   string `json:"class"`
	DocID   string `json:"docId"`
	DocURL  string `json:"docUrl"`
	Skipped bool   `json:"skipped"`
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

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("generate reports failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil || meta == nil || meta.NotesID == "" || meta.ReportsID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not configured, run setup first"})
		return
	}

	generator, err := serviceDeps.GetReportGenerator(svc)
	if err != nil {
		log.Error("generate reports: init failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var reports []reportResult

	// Sequential generation with fail-fast.
	for _, s := range req.Students {
		resp, err := generator.Generate(ctx, GenerateReportRequest{
			Student:          s.Name,
			Class:            s.Class,
			StartDate:        req.StartDate,
			EndDate:          req.EndDate,
			NotesRootID:      meta.NotesID,
			ReportsID:        meta.ReportsID,
			ExamplesFolderID: meta.ReportExamplesID,
			Instructions:     req.Instructions,
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
			Student: s.Name,
			Class:   s.Class,
			DocID:   resp.DocID,
			DocURL:  resp.DocURL,
			Skipped: resp.Skipped,
		})
	}

	log.Info("generate reports completed", "user_id", svc.User.UserID, "report_count", len(reports))
	writeJSON(w, http.StatusOK, generateReportsResponse{
		Reports: reports,
		Error:   nil,
	})
}

type regenerateReportRequest struct {
	DocID        string `json:"docId"`
	Student      string `json:"student"`
	Class        string `json:"class"`
	StartDate    string `json:"startDate"`
	EndDate      string `json:"endDate"`
	Instructions string `json:"instructions"`
}

func handleRegenerateReport(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req regenerateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.DocID == "" || req.Student == "" || req.Class == "" || req.StartDate == "" || req.EndDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
		return
	}

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("regenerate report failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil || meta == nil || meta.NotesID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not configured, run setup first"})
		return
	}

	generator, err := serviceDeps.GetReportGenerator(svc)
	if err != nil {
		log.Error("regenerate report: init failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp, err := generator.Regenerate(ctx, RegenerateReportRequest{
		DocID:            req.DocID,
		Student:          req.Student,
		Class:            req.Class,
		StartDate:        req.StartDate,
		EndDate:          req.EndDate,
		NotesRootID:      meta.NotesID,
		ExamplesFolderID: meta.ReportExamplesID,
		Instructions:     req.Instructions,
	})
	if err != nil {
		log.Error("regenerate report failed", "student", req.Student, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	log.Info("regenerate report completed", "user_id", svc.User.UserID, "doc_id", resp.DocID)
	writeJSON(w, http.StatusOK, resp)
}
