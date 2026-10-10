package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runReportBatch generates reports for n students in one Class, failing the
// students at the given positions, and returns the student IDs, the
// generator's call log, and the response rows.
func runReportBatch(t *testing.T, n int, failAt ...int) (ids, calls []int64, rows []ReportBatchStudent) {
	t.Helper()
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	levelRepo := &LevelRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Math", "")
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "Write three sections."))

	ids = make([]int64, n)
	body := make([]map[string]any, n)
	for i := range n {
		name := fmt.Sprintf("Student%d", i)
		stu, err := studentRepo.Create(ctx, cls.ID, name)
		require.NoError(t, err)
		ids[i] = stu.ID
		body[i] = map[string]any{"studentId": stu.ID, "name": name, "className": "Math"}
	}
	gen := &stubReportGenerator{generateErrs: map[int64]error{}}
	for _, i := range failAt {
		gen.generateErrs[ids[i]] = errors.New("provider said: Student secret")
	}
	withDeps(t, &mockDepsAll{
		db:          db,
		classRepo:   classRepo,
		studentRepo: studentRepo,
		reportRepo:  &ReportRepo{db: db},
		reportGen:   gen,
		levelRepo:   levelRepo,
	})

	reqBody, err := json.Marshal(map[string]any{"students": body, "startDate": "2026-01-01", "endDate": "2026-03-31"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")
	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	var resp GenerateReportsHTTPResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Students, n)
	for i, row := range resp.Students {
		assert.Equal(t, ids[i], row.StudentID, "rows keep request order")
	}
	return ids, gen.genCalls, resp.Students
}

func assertDone(t *testing.T, row ReportBatchStudent) {
	t.Helper()
	assert.Equal(t, ReportStudentDone, row.Status)
	require.NotNil(t, row.ReportID)
	assert.Equal(t, 1000+row.StudentID, *row.ReportID)
	assert.Nil(t, row.Error)
}

func assertFailed(t *testing.T, row ReportBatchStudent, msg string) {
	t.Helper()
	assert.Equal(t, ReportStudentFailed, row.Status)
	assert.Nil(t, row.ReportID)
	require.NotNil(t, row.Error)
	assert.Equal(t, msg, *row.Error, "fixed text: no provider error, no student name")
}

func TestHandleGenerateReports_FailureDoesNotStopBatch(t *testing.T) {
	ids, calls, rows := runReportBatch(t, 3, 0)

	assert.Equal(t, ids, calls)
	assertFailed(t, rows[0], reportStudentErrGenerate)
	assertDone(t, rows[1])
	assertDone(t, rows[2])
}

func TestHandleGenerateReports_SuccessResetsFailureCount(t *testing.T) {
	ids, calls, rows := runReportBatch(t, 5, 0, 2, 3)

	assert.Equal(t, ids, calls)
	assertFailed(t, rows[0], reportStudentErrGenerate)
	assertDone(t, rows[1])
	assertFailed(t, rows[2], reportStudentErrGenerate)
	assertFailed(t, rows[3], reportStudentErrGenerate)
	assertDone(t, rows[4])
}

func TestHandleGenerateReports_ThreeConsecutiveFailuresSkipRest(t *testing.T) {
	ids, calls, rows := runReportBatch(t, 5, 0, 1, 2)

	assert.Equal(t, ids[:3], calls, "generator not called after the third consecutive failure")
	for _, row := range rows[:3] {
		assertFailed(t, row, reportStudentErrGenerate)
	}
	for _, row := range rows[3:] {
		assertFailed(t, row, reportStudentErrSkipped)
	}
}
