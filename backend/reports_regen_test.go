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
	generateResp *GenerateReportResponse
	generateErr  error
	// generateErrs, when set, fails the listed students and answers the rest
	// with ReportID = 1000 + StudentID, so tests can check row-to-report mapping.
	generateErrs map[int64]error
	genCalls     []int64
	// genCallers is the llm_calls user each Generate ctx carried.
	genCallers     []string
	regenerateResp *GenerateReportResponse
	regenerateErr  error
	lastGenReq     GenerateReportRequest
	lastRegenReq   RegenerateReportRequest
}

func (s *stubReportGenerator) Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	s.lastGenReq = req
	s.genCalls = append(s.genCalls, req.StudentID)
	var caller string
	if c, ok := ctx.Value(llmCallerKey).(llmCaller); ok {
		caller = c.userID
	}
	s.genCallers = append(s.genCallers, caller)
	if s.generateErrs != nil {
		if err := s.generateErrs[req.StudentID]; err != nil {
			return nil, err
		}
		return &GenerateReportResponse{ReportID: 1000 + req.StudentID}, nil
	}
	return s.generateResp, s.generateErr
}

func (s *stubReportGenerator) Regenerate(_ context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error) {
	s.lastRegenReq = req
	return s.regenerateResp, s.regenerateErr
}

func clerkReq(r *http.Request, userID string) *http.Request {
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
		Claims: clerk.Claims{
			ActiveOrganizationID:   "test-group",
			ActiveOrganizationRole: "org:member",
		},
	})
	return r.WithContext(ctx)
}

func TestHandleRegenerateReport_LooksUpFromDB(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Thursday Timezone", "")
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
	require.NoError(t, (&NoteRepo{db: db}).Create(ctx, &Note{StudentID: stu.ID, Date: "2026-02-01", Summary: "in range", Source: "manual"}))

	gen := &stubReportGenerator{
		regenerateResp: &GenerateReportResponse{ReportID: 99, HTML: "<p>new</p>"},
	}

	levelRepo := &LevelRepo{db: db}
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "Write three sections."))
	withDeps(t, &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  reportRepo,
		reportGen:   gen,
		levelRepo:   levelRepo,
	})

	body, err := json.Marshal(map[string]string{"feedback": "make it shorter"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader(body))
	req.SetPathValue("id", itoa(rpt.ID))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	assert.Equal(t, "Maxence", gen.lastRegenReq.StudentName)
	assert.Equal(t, "Thursday Timezone · Mon", gen.lastRegenReq.ClassName)
	assert.Equal(t, "2026-01-01", gen.lastRegenReq.StartDate)
	assert.Equal(t, "make it shorter", gen.lastRegenReq.Feedback)
	assert.Equal(t, "be concise", gen.lastRegenReq.Instructions)
	assert.Equal(t, "Write three sections.", gen.lastRegenReq.ReportInstructions)
	require.Len(t, gen.lastRegenReq.Notes, 1)
	assert.Equal(t, "in range", gen.lastRegenReq.Notes[0].Summary)
}

func TestHandleGenerateReports_ResponseShape(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	noteRepo := &NoteRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Art", "")
	stu, err := studentRepo.Create(ctx, cls.ID, "Alice")
	require.NoError(t, err)
	require.NoError(t, noteRepo.Create(ctx, &Note{StudentID: stu.ID, Date: "2026-02-01", Summary: "in range", Source: "manual"}))

	gen := &stubReportGenerator{
		generateResp: &GenerateReportResponse{ReportID: 42, HTML: "<p>hi</p>", CreatedAt: "2026-04-02T00:00:00Z"},
	}

	levelRepo := &LevelRepo{db: db}
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "Write three sections."))
	d := &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		noteRepo:    noteRepo,
		reportRepo:  reportRepo,
		reportGen:   gen,
		levelRepo:   levelRepo,
	}
	withDeps(t, d)

	reqBody, err := json.Marshal(map[string]any{
		"students":     []map[string]any{{"studentId": stu.ID, "name": "Alice", "className": "Art"}},
		"startDate":    "2026-01-01",
		"endDate":      "2026-03-31",
		"instructions": "secret instructions",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, "body = %s", rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "secret instructions", "instructions stay server-side")
	assert.NotContains(t, rec.Body.String(), "in range", "notes stay server-side")

	var batch struct {
		ID        string `json:"id"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		Status    string `json:"status"`
		Students  []struct {
			StudentID int64   `json:"studentId"`
			Name      string  `json:"name"`
			Status    string  `json:"status"`
			ReportID  *int64  `json:"reportId"`
			Error     *string `json:"error"`
		} `json:"students"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&batch))
	require.NotEmpty(t, batch.ID)
	assert.Equal(t, "2026-01-01", batch.StartDate)
	assert.Equal(t, "2026-03-31", batch.EndDate)
	assert.Equal(t, "running", batch.Status)
	require.Len(t, batch.Students, 1)
	assert.Equal(t, stu.ID, batch.Students[0].StudentID)
	assert.Equal(t, "Alice", batch.Students[0].Name)
	assert.Equal(t, "queued", batch.Students[0].Status)
	assert.Empty(t, gen.genCalls, "nothing generated inside the request")

	key := "user_abc/" + batch.ID
	queue, err := d.GetReportBatchQueue()
	require.NoError(t, err)
	require.NoError(t, processReportBatch(ctx, d, queue, key))
	job, err := queue.GetJob(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, ReportBatchFinished, job.snapshot().Status)
	r := job.Students[0]
	assert.Equal(t, ReportStudentDone, r.Status)
	require.NotNil(t, r.ReportID)
	assert.Equal(t, int64(42), *r.ReportID)
	assert.Nil(t, r.Error)
	require.Len(t, gen.lastGenReq.Notes, 1)
	assert.Equal(t, "in range", gen.lastGenReq.Notes[0].Summary)
	assert.Equal(t, "Write three sections.", gen.lastGenReq.ReportInstructions)
	assert.Equal(t, "secret instructions", gen.lastGenReq.Instructions)
	assert.Equal(t, []string{"user_abc"}, gen.genCallers, "llm_calls keep their user")
}

func TestHandleRegenerateReport_ReportNotFound(t *testing.T) {
	db := setupTestDB(t)
	withDeps(t, &mockDepsAll{
		db:          db,
		classRepo:   &ClassRepo{db: db},
		studentRepo: &StudentRepo{db: db},
		reportRepo:  &ReportRepo{db: db},
	})

	body, err := json.Marshal(map[string]string{"feedback": "x"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		"/reports/99999/regenerate", bytes.NewReader(body))
	req.SetPathValue("id", "99999")
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

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Science", "")
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
	levelRepo := &LevelRepo{db: db}
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "Write three sections."))
	withDeps(t, &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo, reportGen: gen, levelRepo: levelRepo,
	})

	body, err := json.Marshal(map[string]string{"feedback": "shorter"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader(body))
	req.SetPathValue("id", itoa(rpt.ID))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var resp struct {
		ID        int64  `json:"id"`
		StudentID int64  `json:"studentId"`
		Student   string `json:"student"`
		ClassName string `json:"className"`
		HTML      string `json:"html"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		CreatedAt string `json:"createdAt"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(77), resp.ID)
	assert.Equal(t, stu.ID, resp.StudentID)
	assert.Equal(t, "Bob", resp.Student)
	assert.Equal(t, "Science · Mon", resp.ClassName)
	// The regenerated body, not the stored "<p>old</p>" — returning the stale
	// report is the one failure this endpoint exists to not have.
	assert.Equal(t, "<p>new</p>", resp.HTML)
	assert.Equal(t, "2026-02-01", resp.StartDate)
	assert.Equal(t, "2026-02-28", resp.EndDate)
	assert.Equal(t, "2026-04-03T00:00:00Z", resp.CreatedAt)
	assert.Equal(t, "Write three sections.", gen.lastRegenReq.ReportInstructions)
}

func TestHandleGetReport_IncludesStudentAndClass(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "History", "")
	stu, err := studentRepo.Create(ctx, cls.ID, "Carol")
	require.NoError(t, err)
	rpt := &Report{StudentID: stu.ID, StartDate: "2026-01-01", EndDate: "2026-03-31", HTML: "<p>report</p>"}
	require.NoError(t, reportRepo.Create(ctx, rpt))

	withDeps(t, &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo,
	})

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/reports/%d", rpt.ID), http.NoBody)
	req.SetPathValue("id", itoa(rpt.ID))
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGetReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var resp struct {
		ID        int64  `json:"id"`
		Student   string `json:"student"`
		ClassName string `json:"className"`
		HTML      string `json:"html"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Carol", resp.Student)
	assert.Equal(t, "History · Mon", resp.ClassName)
}

// TestHandleRegenerateReport_LevelOutsideGroup covers a resolver load error
// failing the request before the generator runs.
func TestHandleRegenerateReport_LevelOutsideGroup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	cls := newTestClass(t, &ClassRepo{db: db}, "other-group", "user_abc", "Elsewhere", "")
	stu, err := (&StudentRepo{db: db}).Create(ctx, cls.ID, "Alice")
	require.NoError(t, err)
	rpt := &Report{StudentID: stu.ID, StartDate: "2026-01-01", EndDate: "2026-03-31", HTML: "<p>old</p>"}
	require.NoError(t, (&ReportRepo{db: db}).Create(ctx, rpt))
	gen := &stubReportGenerator{}
	withDeps(t, &mockDepsAll{
		db:          db,
		studentRepo: &StudentRepo{db: db},
		reportRepo:  &ReportRepo{db: db},
		reportGen:   gen,
	})

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("id", itoa(rpt.ID))
	req = clerkReq(req, "user_abc")
	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, "body = %s", rec.Body.String())
	assert.Empty(t, gen.lastRegenReq.StudentName, "generator must not be called")
}

// cancelingProvider cancels the request mid-call, as a Teacher closing the tab
// would, and fails like a real HTTP client when its context is cancelled.
type cancelingProvider struct {
	fakeLLMProvider
	cancel context.CancelFunc
}

func (p *cancelingProvider) ChatText(ctx context.Context, req ChatTextRequest) (LLMResponse, error) {
	p.cancel()
	if err := ctx.Err(); err != nil {
		return LLMResponse{}, err
	}
	return p.fakeLLMProvider.ChatText(ctx, req)
}

func TestHandleRegenerateReport_SurvivesClientCancel(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	feedbackRepo := &ArtifactFeedbackRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Science", "")
	stu, err := studentRepo.Create(ctx, cls.ID, "Bob")
	require.NoError(t, err)
	rpt := &Report{StudentID: stu.ID, StartDate: "2026-02-01", EndDate: "2026-02-28", HTML: "<p>old</p>"}
	require.NoError(t, reportRepo.Create(ctx, rpt))
	levelRepo := &LevelRepo{db: db}
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "Write three sections."))

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	gen, err := newDBReportGenerator(&cancelingProvider{fakeLLMProvider: fakeLLMProvider{text: "<p>new</p>"}, cancel: cancel}, reportRepo)
	require.NoError(t, err)
	withDeps(t, &mockDepsAll{
		db: db, classRepo: classRepo, studentRepo: studentRepo, reportRepo: reportRepo,
		reportGen: gen, levelRepo: levelRepo, feedbackRepo: feedbackRepo,
	})

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader([]byte(`{"feedback":"shorter"}`)))
	req.SetPathValue("id", itoa(rpt.ID))
	req = clerkReq(req.WithContext(reqCtx), "user_abc")
	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	reports, err := reportRepo.List(ctx, stu.ID)
	require.NoError(t, err)
	assert.Len(t, reports, 2, "new report saved next to the old one")
	fb, err := feedbackRepo.ListByArtifact(ctx, "report", rpt.ID)
	require.NoError(t, err)
	require.Len(t, fb, 1)
	assert.Equal(t, "down", fb[0].Rating)
	assert.Equal(t, "regenerated", fb[0].Signal)
}
