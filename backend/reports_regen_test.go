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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	stu, err := studentRepo.Create(ctx, cls.ID, "Maxence")
	require.NoError(t, err)
	instructions := "be concise"
	rpt := &Report{
		StudentID:    stu.ID,
		StartDate:    "2026-01-01",
		EndDate:      "2026-03-31",
		HTML:         "<p>old</p>",
		Instructions: &instructions,
	}
	require.NoError(t, reportRepo.Create(ctx, rpt))

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
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	assert.Equal(t, "Maxence", gen.lastRegenReq.Student)
	assert.Equal(t, "Thursday Timezone", gen.lastRegenReq.Class)
	assert.Equal(t, "2026-01-01", gen.lastRegenReq.StartDate)
	assert.Equal(t, "make it shorter", gen.lastRegenReq.Feedback)
	assert.Equal(t, "be concise", gen.lastRegenReq.Instructions)
}

func TestHandleGenerateReports_ResponseShape(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	noteRepo := &NoteRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "Art", "")
	require.NoError(t, err)
	stu, err := studentRepo.Create(ctx, cls.ID, "Alice")
	require.NoError(t, err)

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
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

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
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Reports, 1)
	r := resp.Reports[0]
	assert.Equal(t, int64(42), r.ID)
	assert.Equal(t, stu.ID, r.StudentID)
	assert.Equal(t, "Alice", r.Student)
	assert.Equal(t, "2026-01-01", r.StartDate)
	assert.Equal(t, "2026-03-31", r.EndDate)
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
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		"/reports/99999/regenerate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "body = %s", rec.Body.String())
}

func TestHandleRegenerateReport_ResponseShape(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "Science", "")
	require.NoError(t, err)
	stu, err := studentRepo.Create(ctx, cls.ID, "Bob")
	require.NoError(t, err)
	rpt := &Report{
		StudentID: stu.ID,
		StartDate: "2026-02-01",
		EndDate:   "2026-02-28",
		HTML:      "<p>old</p>",
	}
	require.NoError(t, reportRepo.Create(ctx, rpt))

	gen := &stubReportGenerator{
		regenerateResp: &GenerateReportResponse{ReportID: 77, HTML: "<p>new</p>", CreatedAt: "2026-04-03T00:00:00Z"},
	}
	serviceDeps = &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo, reportGen: gen,
	}

	body, err := json.Marshal(map[string]string{"feedback": "shorter"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

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
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(77), resp.ID)
	assert.Equal(t, "Bob", resp.Student)
	assert.Equal(t, "Science", resp.Class)
	assert.Equal(t, "2026-02-01", resp.StartDate)
}

func TestHandleGetReport_IncludesStudentAndClass(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls, err := classRepo.Create(ctx, "user_abc", "History", "")
	require.NoError(t, err)
	stu, err := studentRepo.Create(ctx, cls.ID, "Carol")
	require.NoError(t, err)
	rpt := &Report{StudentID: stu.ID, StartDate: "2026-01-01", EndDate: "2026-03-31", HTML: "<p>report</p>"}
	require.NoError(t, reportRepo.Create(ctx, rpt))

	serviceDeps = &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo,
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/reports/%d", rpt.ID), http.NoBody)
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGetReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var resp struct {
		ID      int64  `json:"id"`
		Student string `json:"student"`
		Class   string `json:"class"`
		HTML    string `json:"html"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Carol", resp.Student)
	assert.Equal(t, "History", resp.Class)
}
