package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reportBatchFixture is one Teacher with n students in a Class whose Level
// has report instructions, and deps wired to a report batch queue.
type reportBatchFixture struct {
	ids   []int64
	body  []map[string]any
	gen   *stubReportGenerator
	deps  *mockDepsAll
	queue *MemQueue[ReportJob]
}

// newReportBatchFixture seeds n students, failing generation for the students
// at the given positions. The queue has no worker: tests run
// processReportBatch themselves.
func newReportBatchFixture(t *testing.T, n int, failAt ...int) *reportBatchFixture {
	t.Helper()
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	levelRepo := &LevelRepo{db: db}
	ctx := context.Background()

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Math", "")
	require.NoError(t, levelRepo.UpdateReportInstructions(ctx, "test-group", cls.LevelID, "Write three sections."))

	f := &reportBatchFixture{ids: make([]int64, n), body: make([]map[string]any, n)}
	for i := range n {
		name := fmt.Sprintf("Student%d", i)
		stu, err := studentRepo.Create(ctx, cls.ID, name)
		require.NoError(t, err)
		f.ids[i] = stu.ID
		f.body[i] = map[string]any{"studentId": stu.ID, "name": name, "className": "Math"}
	}
	f.gen = &stubReportGenerator{generateErrs: map[int64]error{}}
	for _, i := range failAt {
		f.gen.generateErrs[f.ids[i]] = errors.New("provider said: Student secret")
	}
	f.queue = NewMemQueue[ReportJob](nil, 0)
	f.deps = &mockDepsAll{
		db:               db,
		classRepo:        classRepo,
		studentRepo:      studentRepo,
		reportRepo:       &ReportRepo{db: db},
		feedbackRepo:     &ArtifactFeedbackRepo{db: db},
		reportGen:        f.gen,
		levelRepo:        levelRepo,
		reportBatchQueue: f.queue,
	}
	withDeps(t, f.deps)
	return f
}

// post sends POST /reports for every fixture student.
func (f *reportBatchFixture) post(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	reqBody, err := json.Marshal(map[string]any{"students": f.body, "startDate": "2026-01-01", "endDate": "2026-03-31"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = clerkReq(req, "user_abc")
	rec := httptest.NewRecorder()
	handleGenerateReports(rec, req)
	return rec
}

// postAccepted posts and returns the 202 snapshot.
func (f *reportBatchFixture) postAccepted(t *testing.T) ReportBatch {
	t.Helper()
	rec := f.post(t)
	require.Equal(t, http.StatusAccepted, rec.Code, "body = %s", rec.Body.String())
	var batch ReportBatch
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&batch))
	return batch
}

func listBatches(t *testing.T) []ReportBatch {
	t.Helper()
	req := clerkReq(httptest.NewRequest(http.MethodGet, "/reports/batches", http.NoBody), "user_abc")
	rec := httptest.NewRecorder()
	handleListReportBatches(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	var resp ListReportBatchesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp.Batches
}

// runReportBatch posts a batch of n students, failing those at failAt, runs
// the worker on it, and returns the student IDs, the generator's call log,
// and the final rows.
func runReportBatch(t *testing.T, n int, failAt ...int) (ids, calls []int64, rows []ReportBatchStudent) {
	t.Helper()
	f := newReportBatchFixture(t, n, failAt...)
	batch := f.postAccepted(t)
	ctx := context.Background()
	key := "user_abc/" + batch.ID
	require.NoError(t, processReportBatch(ctx, f.deps, f.queue, key))
	job, err := f.queue.GetJob(ctx, key)
	require.NoError(t, err)
	require.Len(t, job.Students, n)
	for i, row := range job.Students {
		assert.Equal(t, f.ids[i], row.StudentID, "rows keep request order")
	}
	return f.ids, f.gen.genCalls, job.Students
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

func TestHandleGenerateReports_RealQueueRunsBatch(t *testing.T) {
	f := newReportBatchFixture(t, 4, 1)
	f.queue = NewMemQueue[ReportJob](func(ctx context.Context, q JobQueue[ReportJob], key string) error {
		return processReportBatch(ctx, f.deps, q, key)
	}, 1)
	t.Cleanup(f.queue.Close)
	f.deps.reportBatchQueue = f.queue
	batch := f.postAccepted(t)
	assert.Equal(t, ReportBatchRunning, batch.Status)

	// Poll while the worker writes, so -race sees reads against row updates.
	var last ReportBatch
	require.Eventually(t, func() bool {
		jobs, err := f.queue.ListJobs(context.Background(), "user_abc")
		if err != nil || len(jobs) != 1 {
			return false
		}
		last = jobs[0].snapshot()
		return last.Status == ReportBatchFinished
	}, 5*time.Second, time.Millisecond)

	assert.Equal(t, batch.ID, last.ID)
	assert.Equal(t, f.ids, f.gen.genCalls)
	assertDone(t, last.Students[0])
	assertFailed(t, last.Students[1], reportStudentErrGenerate)
	assertDone(t, last.Students[2])
	assertDone(t, last.Students[3])
}

func TestHandleGenerateReports_ConflictWhileRunning(t *testing.T) {
	f := newReportBatchFixture(t, 2)
	first := f.postAccepted(t)

	rec := f.post(t)
	require.Equal(t, http.StatusConflict, rec.Code, "body = %s", rec.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, first.ID, resp["batchId"])

	jobs, err := f.queue.ListJobs(context.Background(), "user_abc")
	require.NoError(t, err)
	assert.Len(t, jobs, 1, "the running batch is untouched")
}

func TestHandleGenerateReports_ConflictWhileLocked(t *testing.T) {
	f := newReportBatchFixture(t, 1)
	require.True(t, takeUploadLock("report-batch/user_abc"))
	t.Cleanup(func() { releaseUploadLock("report-batch/user_abc") })

	rec := f.post(t)
	require.Equal(t, http.StatusConflict, rec.Code, "body = %s", rec.Body.String())
	jobs, err := f.queue.ListJobs(context.Background(), "user_abc")
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestHandleGenerateReports_PrunesFinishedBatch(t *testing.T) {
	f := newReportBatchFixture(t, 1)
	ctx := context.Background()
	first := f.postAccepted(t)
	require.NoError(t, processReportBatch(ctx, f.deps, f.queue, "user_abc/"+first.ID))

	second := f.postAccepted(t)

	batches := listBatches(t)
	require.Len(t, batches, 1)
	assert.Equal(t, second.ID, batches[0].ID)
}

// failingPublishQueue stores the job and then fails, as MemQueue does when its
// channel is full.
type failingPublishQueue struct{ *MemQueue[ReportJob] }

func (q failingPublishQueue) Publish(ctx context.Context, job ReportJob) error {
	if err := q.UpdateJob(ctx, job); err != nil {
		return err
	}
	return errors.New("work channel full")
}

func TestHandleGenerateReports_QueueFullKeepsPreviousBatch(t *testing.T) {
	f := newReportBatchFixture(t, 1)
	ctx := context.Background()
	first := f.postAccepted(t)
	require.NoError(t, processReportBatch(ctx, f.deps, f.queue, "user_abc/"+first.ID))

	f.deps.reportBatchQueue = failingPublishQueue{f.queue}
	rec := f.post(t)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "body = %s", rec.Body.String())

	batches := listBatches(t)
	require.Len(t, batches, 1, "the failed batch is removed, or it would block every later POST")
	assert.Equal(t, first.ID, batches[0].ID, "previous results survive a 503")
}

func TestHandleListReportBatches_NewestFirstAndOwnerOnly(t *testing.T) {
	f := newReportBatchFixture(t, 1)
	ctx := context.Background()
	now := time.Now()
	for i, job := range []ReportJob{
		{UserID: "user_abc", BatchID: "old", CreatedAt: now.Add(-time.Hour)},
		{UserID: "user_abc", BatchID: "new", CreatedAt: now},
		{UserID: "user_other", BatchID: "theirs", CreatedAt: now},
	} {
		require.NoError(t, f.queue.UpdateJob(ctx, job), "job %d", i)
	}

	batches := listBatches(t)
	require.Len(t, batches, 2)
	assert.Equal(t, "new", batches[0].ID)
	assert.Equal(t, "old", batches[1].ID)
}

func TestHandleListReportBatches_EmptyIsList(t *testing.T) {
	newReportBatchFixture(t, 0)
	req := clerkReq(httptest.NewRequest(http.MethodGet, "/reports/batches", http.NoBody), "user_abc")
	rec := httptest.NewRecorder()
	handleListReportBatches(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"batches":[]}`, rec.Body.String())
}

func TestProcessReportBatch_StopsOnCancel(t *testing.T) {
	f := newReportBatchFixture(t, 2)
	batch := f.postAccepted(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := processReportBatch(ctx, f.deps, f.queue, "user_abc/"+batch.ID)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, f.gen.genCalls)
}

func TestProcessReportBatch_GeneratorInitFailsEveryRow(t *testing.T) {
	f := newReportBatchFixture(t, 2)
	batch := f.postAccepted(t)
	f.deps.reportGenErr = errors.New("no provider")
	ctx := context.Background()
	key := "user_abc/" + batch.ID

	require.NoError(t, processReportBatch(ctx, f.deps, f.queue, key))
	job, err := f.queue.GetJob(ctx, key)
	require.NoError(t, err)
	for _, row := range job.Students {
		assertFailed(t, row, reportStudentErrGenerate)
	}
}

func retryBatch(userID, batchID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/reports/batches/"+batchID+"/retry", http.NoBody)
	req.SetPathValue("id", batchID)
	rec := httptest.NewRecorder()
	handleRetryReportBatch(rec, clerkReq(req, userID))
	return rec
}

func (f *reportBatchFixture) finishedBatch(t *testing.T) (id, key string) {
	t.Helper()
	batch := f.postAccepted(t)
	key = reportBatchKey("user_abc", batch.ID)
	require.NoError(t, processReportBatch(context.Background(), f.deps, f.queue, key))
	return batch.ID, key
}

func TestHandleRetryReportBatch_RequeuesOnlyFailedRows(t *testing.T) {
	// Row 0 done; rows 1–3 fail in a row, so row 4 is skipped.
	f := newReportBatchFixture(t, 5, 1, 2, 3)
	id, key := f.finishedBatch(t)

	rec := retryBatch("user_abc", id)
	require.Equal(t, http.StatusAccepted, rec.Code, "body = %s", rec.Body.String())
	var snap ReportBatch
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&snap))
	assert.Equal(t, id, snap.ID)
	assert.Equal(t, ReportBatchRunning, snap.Status)
	assertDone(t, snap.Students[0])
	for _, row := range snap.Students[1:] {
		assert.Equal(t, ReportStudentQueued, row.Status)
		assert.Nil(t, row.Error)
	}

	f.gen.generateErrs = map[int64]error{}
	f.gen.genCalls = nil
	require.NoError(t, processReportBatch(context.Background(), f.deps, f.queue, key))

	assert.Equal(t, f.ids[1:], f.gen.genCalls, "done rows are not generated again")
	job, err := f.queue.GetJob(context.Background(), key)
	require.NoError(t, err)
	for _, row := range job.Students {
		assertDone(t, row)
	}
}

func TestHandleRetryReportBatch_ConflictWhileRunning(t *testing.T) {
	f := newReportBatchFixture(t, 1)
	batch := f.postAccepted(t)

	rec := retryBatch("user_abc", batch.ID)
	require.Equal(t, http.StatusConflict, rec.Code, "body = %s", rec.Body.String())
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, batch.ID, resp["batchId"])
}

func TestHandleRetryReportBatch_ConflictWhileLocked(t *testing.T) {
	f := newReportBatchFixture(t, 1, 0)
	id, key := f.finishedBatch(t)
	require.True(t, takeUploadLock("report-batch/user_abc"))
	t.Cleanup(func() { releaseUploadLock("report-batch/user_abc") })

	rec := retryBatch("user_abc", id)
	require.Equal(t, http.StatusConflict, rec.Code, "body = %s", rec.Body.String())
	job, err := f.queue.GetJob(context.Background(), key)
	require.NoError(t, err)
	assertFailed(t, job.Students[0], reportStudentErrGenerate)
}

func TestHandleRetryReportBatch_NoFailedRows(t *testing.T) {
	f := newReportBatchFixture(t, 1)
	id, _ := f.finishedBatch(t)

	rec := retryBatch("user_abc", id)
	assert.Equal(t, http.StatusConflict, rec.Code, "body = %s", rec.Body.String())
}

func TestHandleRetryReportBatch_NotFound(t *testing.T) {
	f := newReportBatchFixture(t, 1, 0)
	id, key := f.finishedBatch(t)

	for _, c := range []struct{ name, user, id string }{
		{"unknown id", "user_abc", "nope"},
		{"another Teacher's batch", "user_other", id},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := retryBatch(c.user, c.id)
			require.Equal(t, http.StatusNotFound, rec.Code, "body = %s", rec.Body.String())
			assert.JSONEq(t, `{"error":"report batch not found"}`, rec.Body.String())
		})
	}
	job, err := f.queue.GetJob(context.Background(), key)
	require.NoError(t, err)
	assertFailed(t, job.Students[0], reportStudentErrGenerate)
}

func TestHandleRetryReportBatch_QueueFullRestoresFailedRows(t *testing.T) {
	f := newReportBatchFixture(t, 5, 1, 2, 3)
	id, _ := f.finishedBatch(t)

	f.deps.reportBatchQueue = failingPublishQueue{f.queue}
	rec := retryBatch("user_abc", id)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "body = %s", rec.Body.String())

	batches := listBatches(t)
	require.Len(t, batches, 1)
	assert.Equal(t, ReportBatchFinished, batches[0].Status, "a running batch would block every later POST")
	assertDone(t, batches[0].Students[0])
	assertFailed(t, batches[0].Students[1], reportStudentErrGenerate)
	assertFailed(t, batches[0].Students[2], reportStudentErrGenerate)
	assertFailed(t, batches[0].Students[3], reportStudentErrGenerate)
	assertFailed(t, batches[0].Students[4], reportStudentErrSkipped)
}

// Two clicks race each other and a poll: one wins, one publish.
func TestHandleRetryReportBatch_DoubleClick(t *testing.T) {
	f := newReportBatchFixture(t, 2, 0)
	// Concurrent handlers query the DB; a second :memory: connection is empty.
	f.deps.db.SetMaxOpenConns(1)
	id, _ := f.finishedBatch(t)
	published := len(f.queue.work)

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = retryBatch("user_abc", id).Code })
	}
	wg.Go(func() {
		req := clerkReq(httptest.NewRequest(http.MethodGet, "/reports/batches", http.NoBody), "user_abc")
		handleListReportBatches(httptest.NewRecorder(), req)
	})
	wg.Wait()

	assert.ElementsMatch(t, []int{http.StatusAccepted, http.StatusConflict}, codes)
	assert.Equal(t, published+1, len(f.queue.work), "one retry published")
}

func TestHandleListReportBatches_RowOpensLatestDraft(t *testing.T) {
	f := newReportBatchFixture(t, 2)
	ctx := context.Background()
	reports := f.deps.reportRepo
	draft := func(source int64) int64 {
		t.Helper()
		rpt := &Report{StudentID: f.ids[0], StartDate: "2026-01-01", EndDate: "2026-03-31", HTML: "<p>x</p>"}
		if source == 0 {
			require.NoError(t, reports.Create(ctx, rpt))
			return rpt.ID
		}
		fb := ArtifactFeedback{ArtifactType: "report", ArtifactID: source, Rating: "down", Signal: "regenerated", UserID: "user_abc"}
		require.NoError(t, reports.CreateRegenerated(ctx, rpt, fb))
		return rpt.ID
	}
	root := draft(0)
	failed := reportStudentErrGenerate
	require.NoError(t, f.queue.UpdateJob(ctx, ReportJob{
		UserID: "user_abc", BatchID: "b", CreatedAt: time.Now(),
		Students: []ReportBatchStudent{
			{StudentID: f.ids[0], Status: ReportStudentDone, ReportID: &root},
			{StudentID: f.ids[1], Status: ReportStudentFailed, Error: &failed},
		},
	}))
	rowReport := func() int64 {
		t.Helper()
		batches := listBatches(t)
		require.Len(t, batches, 1)
		require.NotNil(t, batches[0].Students[0].ReportID)
		return *batches[0].Students[0].ReportID
	}

	assert.Equal(t, root, rowReport(), "no regenerates")
	a := draft(root)
	b := draft(a)
	assert.Equal(t, b, rowReport(), "follows the chain")
	c := draft(root)
	assert.Equal(t, c, rowReport(), "newest branch wins")
	d := draft(a)
	assert.Equal(t, d, rowReport(), "newest descendant wins, on any branch")
	require.NoError(t, reports.Delete(ctx, d))
	assert.Equal(t, c, rowReport(), "deleted latest falls back to the newest survivor")
	require.NoError(t, reports.Delete(ctx, a))
	assert.Equal(t, c, rowReport(), "a deleted middle draft keeps its descendants")
	require.NoError(t, reports.Delete(ctx, c))
	assert.Equal(t, b, rowReport(), "b survives behind deleted a")

	rec := retryBatch("user_abc", "b")
	require.Equal(t, http.StatusAccepted, rec.Code, "body = %s", rec.Body.String())
	var snap ReportBatch
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&snap))
	assert.Equal(t, b, *snap.Students[0].ReportID, "retry answers with latest drafts too")
	job, err := f.queue.GetJob(ctx, "user_abc/b")
	require.NoError(t, err)
	assert.Equal(t, root, *job.Students[0].ReportID, "stored job keeps the original id")
}
