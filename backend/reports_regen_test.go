package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
)

// stubReportGenerator implements ReportGenerator for tests.
type stubReportGenerator struct {
	generateResp   *GenerateReportResponse
	generateErr    error
	regenerateResp *GenerateReportResponse
	regenerateErr  error
	lastRegenReq   RegenerateReportRequest
}

func (s *stubReportGenerator) Generate(_ context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	return s.generateResp, s.generateErr
}

func (s *stubReportGenerator) Regenerate(_ context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error) {
	s.lastRegenReq = req
	return s.regenerateResp, s.regenerateErr
}

func clerkReq(r *http.Request, userID string) *http.Request {
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
	return r.WithContext(ctx)
}

func TestHandleRegenerateReport_LooksUpFromDB(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "Thursday Timezone")
	if err != nil {
		t.Fatal(err)
	}
	stu, err := studentRepo.Create(ctx, cls.ID, "Maxence")
	if err != nil {
		t.Fatal(err)
	}
	instructions := "be concise"
	rpt := &Report{
		StudentID:    stu.ID,
		StartDate:    "2026-01-01",
		EndDate:      "2026-03-31",
		HTML:         "<p>old</p>",
		Instructions: &instructions,
	}
	if err := reportRepo.Create(ctx, rpt); err != nil {
		t.Fatal(err)
	}

	gen := &stubReportGenerator{
		regenerateResp: &GenerateReportResponse{ReportID: 99, HTML: "<p>new</p>"},
	}

	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  reportRepo,
		reportGen:   gen,
	}

	body, err := json.Marshal(map[string]string{"feedback": "make it shorter"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if gen.lastRegenReq.Student != "Maxence" {
		t.Errorf("Student = %q, want Maxence", gen.lastRegenReq.Student)
	}
	if gen.lastRegenReq.Class != "Thursday Timezone" {
		t.Errorf("Class = %q, want Thursday Timezone", gen.lastRegenReq.Class)
	}
	if gen.lastRegenReq.StartDate != "2026-01-01" {
		t.Errorf("StartDate = %q, want 2026-01-01", gen.lastRegenReq.StartDate)
	}
	if gen.lastRegenReq.Feedback != "make it shorter" {
		t.Errorf("Feedback = %q, want 'make it shorter'", gen.lastRegenReq.Feedback)
	}
	if gen.lastRegenReq.Instructions != "be concise" {
		t.Errorf("Instructions = %q, want 'be concise'", gen.lastRegenReq.Instructions)
	}
}

func TestHandleRegenerateReport_ReportNotFound(t *testing.T) {
	db := setupTestDB(t)
	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   &ClassRepo{db: db},
		studentRepo: &StudentRepo{db: db},
		reportRepo:  &ReportRepo{db: db},
	}

	body, err := json.Marshal(map[string]string{"feedback": "x"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/reports/99999/regenerate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}
