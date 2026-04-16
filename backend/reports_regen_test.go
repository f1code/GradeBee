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

	cls, err := classRepo.Create(ctx, "user_abc", "Thursday Timezone", "")
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

func TestHandleGenerateReports_ResponseShape(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	noteRepo := &NoteRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "Art", "")
	if err != nil {
		t.Fatal(err)
	}
	stu, err := studentRepo.Create(ctx, cls.ID, "Alice")
	if err != nil {
		t.Fatal(err)
	}

	gen := &stubReportGenerator{
		generateResp: &GenerateReportResponse{ReportID: 42, HTML: "<p>hi</p>"},
	}

	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		noteRepo:    noteRepo,
		reportRepo:  reportRepo,
		reportGen:   gen,
	}

	reqBody, err := json.Marshal(map[string]any{
		"students":  []map[string]any{{"studentId": stu.ID, "name": "Alice", "class": "Art"}},
		"startDate": "2026-01-01",
		"endDate":   "2026-03-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Reports []struct {
			ID        int64  `json:"id"`
			StudentID int64  `json:"studentId"`
			Student   string `json:"student"`
			Class     string `json:"class"`
			HTML      string `json:"html"`
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
			CreatedAt string `json:"createdAt"`
		} `json:"reports"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(resp.Reports))
	}
	r := resp.Reports[0]
	if r.ID != 42 {
		t.Errorf("id = %d, want 42", r.ID)
	}
	if r.StudentID != stu.ID {
		t.Errorf("studentId = %d, want %d", r.StudentID, stu.ID)
	}
	if r.Student != "Alice" {
		t.Errorf("student = %q, want Alice", r.Student)
	}
	if r.StartDate != "2026-01-01" {
		t.Errorf("startDate = %q, want 2026-01-01", r.StartDate)
	}
	if r.EndDate != "2026-03-31" {
		t.Errorf("endDate = %q, want 2026-03-31", r.EndDate)
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

func TestHandleRegenerateReport_ResponseShape(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "Science", "")
	if err != nil {
		t.Fatal(err)
	}
	stu, err := studentRepo.Create(ctx, cls.ID, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	rpt := &Report{
		StudentID: stu.ID,
		StartDate: "2026-02-01",
		EndDate:   "2026-02-28",
		HTML:      "<p>old</p>",
	}
	if err := reportRepo.Create(ctx, rpt); err != nil {
		t.Fatal(err)
	}

	gen := &stubReportGenerator{
		regenerateResp: &GenerateReportResponse{ReportID: 77, HTML: "<p>new</p>", CreatedAt: "2026-04-03T00:00:00Z"},
	}
	serviceDeps = &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo, reportGen: gen,
	}

	body, err := json.Marshal(map[string]string{"feedback": "shorter"})
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

	var resp struct {
		ID        int64  `json:"id"`
		StudentID int64  `json:"studentId"`
		Student   string `json:"student"`
		Class     string `json:"class"`
		HTML      string `json:"html"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 77 {
		t.Errorf("id = %d, want 77", resp.ID)
	}
	if resp.Student != "Bob" {
		t.Errorf("student = %q, want Bob", resp.Student)
	}
	if resp.Class != "Science" {
		t.Errorf("class = %q, want Science", resp.Class)
	}
	if resp.StartDate != "2026-02-01" {
		t.Errorf("startDate = %q, want 2026-02-01", resp.StartDate)
	}
}

func TestHandleGetReport_IncludesStudentAndClass(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "History", "")
	if err != nil {
		t.Fatal(err)
	}
	stu, err := studentRepo.Create(ctx, cls.ID, "Carol")
	if err != nil {
		t.Fatal(err)
	}
	rpt := &Report{StudentID: stu.ID, StartDate: "2026-01-01", EndDate: "2026-03-31", HTML: "<p>report</p>"}
	if err := reportRepo.Create(ctx, rpt); err != nil {
		t.Fatal(err)
	}

	serviceDeps = &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo,
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/reports/%d", rpt.ID), http.NoBody)
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGetReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID      int64  `json:"id"`
		Student string `json:"student"`
		Class   string `json:"class"`
		HTML    string `json:"html"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Student != "Carol" {
		t.Errorf("student = %q, want Carol", resp.Student)
	}
	if resp.Class != "History" {
		t.Errorf("class = %q, want History", resp.Class)
	}
}
