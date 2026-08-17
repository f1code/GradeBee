package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleGenerateReports_RefusesUnsetLevelInstructions covers the hard
// gate: a Level with no report instructions blocks the whole batch, before
// any report row is created and before the stub generator is ever called.
func TestHandleGenerateReports_RefusesUnsetLevelInstructions(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	levelRepo := &LevelRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Sam", "")
	stu, err := studentRepo.Create(ctx, cls.ID, "Alice")
	require.NoError(t, err)
	// Level "Sam" is left with default empty report_instructions.

	gen := &stubReportGenerator{
		generateResp: &GenerateReportResponse{ReportID: 42, HTML: "<p>hi</p>"},
	}
	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  reportRepo,
		reportGen:   gen,
		levelRepo:   levelRepo,
	}

	reqBody, err := json.Marshal(map[string]any{
		"students":  []map[string]any{{"studentId": stu.ID, "name": "Alice", "className": "Sam"}},
		"startDate": "2026-01-01",
		"endDate":   "2026-03-31",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body = %s", rec.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "Sam")
	assert.Contains(t, resp["error"], "no report instructions")

	reports, err := reportRepo.List(ctx, stu.ID)
	require.NoError(t, err)
	assert.Empty(t, reports, "no report row must be created on gate refusal")
}

// TestHandleGenerateReports_RefusesWhitespaceOnlyInstructions covers
// whitespace-only Report Instructions gating the same as empty.
func TestHandleGenerateReports_RefusesWhitespaceOnlyInstructions(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	levelRepo := &LevelRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Whitespace", "")
	stu, err := studentRepo.Create(ctx, cls.ID, "Alice")
	require.NoError(t, err)
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "   \n\t  "))

	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  reportRepo,
		reportGen:   &stubReportGenerator{},
		levelRepo:   levelRepo,
	}

	reqBody, err := json.Marshal(map[string]any{
		"students":  []map[string]any{{"studentId": stu.ID, "name": "Alice", "className": "Whitespace"}},
		"startDate": "2026-01-01",
		"endDate":   "2026-03-31",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body = %s", rec.Body.String())
}

// TestHandleGenerateReports_MixedLevelBatchRefusesWhole covers a batch
// spanning two Levels where only one is unset: the whole batch is refused,
// not just the offending student, and no report row lands for either Level.
func TestHandleGenerateReports_MixedLevelBatchRefusesWhole(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	levelRepo := &LevelRepo{db: db}
	ctx := context.Background()

	setCls := newTestClass(t, classRepo, "test-group", "user_abc", "SetLevel", "")
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", setCls.LevelID, "Write three sections."))
	stu1, err := studentRepo.Create(ctx, setCls.ID, "Alice")
	require.NoError(t, err)

	unsetCls := newTestClass(t, classRepo, "test-group", "user_abc", "UnsetLevel", "")
	stu2, err := studentRepo.Create(ctx, unsetCls.ID, "Bob")
	require.NoError(t, err)

	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  reportRepo,
		reportGen:   &stubReportGenerator{generateResp: &GenerateReportResponse{ReportID: 1, HTML: "<p>hi</p>"}},
		levelRepo:   levelRepo,
	}

	reqBody, err := json.Marshal(map[string]any{
		"students": []map[string]any{
			{"studentId": stu1.ID, "name": "Alice", "className": "SetLevel"},
			{"studentId": stu2.ID, "name": "Bob", "className": "UnsetLevel"},
		},
		"startDate": "2026-01-01",
		"endDate":   "2026-03-31",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body = %s", rec.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "UnsetLevel")
	assert.NotContains(t, resp["error"], "SetLevel", "only the offending Level is named")

	reports1, err := reportRepo.List(ctx, stu1.ID)
	require.NoError(t, err)
	assert.Empty(t, reports1, "no report for the set Level either — whole batch refused")
	reports2, err := reportRepo.List(ctx, stu2.ID)
	require.NoError(t, err)
	assert.Empty(t, reports2)
}

// TestHandleRegenerateReport_RefusesUnsetLevelInstructions covers the
// regenerate-side hard gate.
func TestHandleRegenerateReport_RefusesUnsetLevelInstructions(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	reportRepo := &ReportRepo{db: db}
	levelRepo := &LevelRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "NoInstructions", "")
	stu, err := studentRepo.Create(ctx, cls.ID, "Maxence")
	require.NoError(t, err)
	rpt := &Report{
		StudentID: stu.ID,
		StartDate: "2026-01-01",
		EndDate:   "2026-03-31",
		HTML:      "<p>old</p>",
	}
	require.NoError(t, reportRepo.Create(ctx, rpt))

	serviceDeps = &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  reportRepo,
		reportGen:   &stubReportGenerator{},
		levelRepo:   levelRepo,
	}

	body, err := json.Marshal(map[string]string{"feedback": "make it shorter"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/reports/%d/regenerate", rpt.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")

	rec := httptest.NewRecorder()
	handleRegenerateReport(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body = %s", rec.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "NoInstructions")

	reports, err := reportRepo.List(ctx, stu.ID)
	require.NoError(t, err)
	assert.Len(t, reports, 1, "only the pre-existing report; no new row from the refused regeneration")
}
