package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLLMProvider is a hand-written LLMProvider for llmReportGenerator tests:
// it records every ChatText prompt and answers with canned text or an error.
// The other methods are never reached by the report path and fail loudly.
type fakeLLMProvider struct {
	text  string
	err   error
	calls []ChatTextRequest
}

var _ LLMProvider = (*fakeLLMProvider)(nil)

// Distinct per-task model names so a generator that stamps the wrong task's
// model onto the row is caught by value.
const (
	fakeReportModel  = "fake-report-model-v7"
	fakeExtractModel = "fake-extract-model-v3"
)

func (f *fakeLLMProvider) Name() string { return "fake" }

func (f *fakeLLMProvider) Model(task LLMTask) string {
	if task == LLMTaskReport {
		return fakeReportModel
	}
	return fakeExtractModel
}

func (f *fakeLLMProvider) ChatText(_ context.Context, req ChatTextRequest) (string, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

func (f *fakeLLMProvider) ChatJSON(_ context.Context, _ ChatJSONRequest, _ any) (string, error) {
	return "", errors.New("fakeLLMProvider: ChatJSON not expected on the report path")
}

func (f *fakeLLMProvider) Vision(_ context.Context, _ VisionRequest, _ any) (string, error) {
	return "", errors.New("fakeLLMProvider: Vision not expected on the report path")
}

func (f *fakeLLMProvider) Transcribe(_ context.Context, _ TranscribeRequest) (TranscribeResponse, error) {
	return TranscribeResponse{}, errors.New("fakeLLMProvider: Transcribe not expected on the report path")
}

// reportGenFixture is one student with notes on both sides of the report
// window, plus a classmate with an in-range note, so the prompt assertions
// can pair every "must not appear" with a "must appear" from the same table.
type reportGenFixture struct {
	ctx        context.Context
	noteRepo   *NoteRepo
	reportRepo *ReportRepo
	studentID  int64
	provider   *fakeLLMProvider
	gen        *llmReportGenerator
}

const (
	fixtureStudent      = "Fenwick"
	fixtureClassmate    = "Ondine"
	fixtureClassName    = "Geology · Mon"
	fixtureStart        = "2026-01-01"
	fixtureEnd          = "2026-03-31"
	noteInRangeJan      = "Fenwick mastered fractions"
	noteInRangeFeb      = "Fenwick led the volcano demo"
	noteBeforeRange     = "Fenwick forgot his homework in December"
	noteAfterRange      = "Fenwick aced the April final"
	noteClassmate       = "Ondine sketched the rock cycle"
	fixtureSpec         = "Write exactly three sections: effort, progress, next steps."
	fixtureInstructions = "Keep it to two paragraphs"
	fixtureFeedback     = "Mention the volcano demo by name"
	cannedHTML          = "<p>Fenwick had a strong term.</p>"
	regeneratedHTML     = "<p>Fenwick's volcano demo was the highlight.</p>"
)

func newReportGenFixture(t *testing.T, provider *fakeLLMProvider) *reportGenFixture {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	noteRepo := &NoteRepo{db: db}
	reportRepo := &ReportRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "user_abc", "Geology", "")
	stu, err := studentRepo.Create(ctx, cls.ID, fixtureStudent)
	require.NoError(t, err)
	mate, err := studentRepo.Create(ctx, cls.ID, fixtureClassmate)
	require.NoError(t, err)

	for _, n := range []Note{
		{StudentID: stu.ID, Date: "2026-01-15", Summary: noteInRangeJan, Source: "voice"},
		{StudentID: stu.ID, Date: "2026-02-20", Summary: noteInRangeFeb, Source: "voice"},
		{StudentID: stu.ID, Date: "2025-12-31", Summary: noteBeforeRange, Source: "voice"},
		{StudentID: stu.ID, Date: "2026-04-01", Summary: noteAfterRange, Source: "voice"},
		{StudentID: mate.ID, Date: "2026-02-01", Summary: noteClassmate, Source: "voice"},
	} {
		require.NoError(t, noteRepo.Create(ctx, &n))
	}

	gen, err := newDBReportGenerator(provider, noteRepo, reportRepo)
	require.NoError(t, err)
	return &reportGenFixture{
		ctx:        ctx,
		noteRepo:   noteRepo,
		reportRepo: reportRepo,
		studentID:  stu.ID,
		provider:   provider,
		gen:        gen,
	}
}

func (f *reportGenFixture) generateReq() GenerateReportRequest {
	return GenerateReportRequest{
		StudentID:          f.studentID,
		Student:            fixtureStudent,
		ClassName:          fixtureClassName,
		StartDate:          fixtureStart,
		EndDate:            fixtureEnd,
		UserID:             "user_abc",
		Instructions:       fixtureInstructions,
		ReportInstructions: fixtureSpec,
	}
}

// singlePrompt returns the one prompt the provider saw, failing on any other count.
func (f *reportGenFixture) singlePrompt(t *testing.T) string {
	t.Helper()
	require.Len(t, f.provider.calls, 1, "exactly one LLM call per generation")
	return f.provider.calls[0].UserPrompt
}

func TestLLMReportGenerator_Generate_PromptAndPersistedRow(t *testing.T) {
	f := newReportGenFixture(t, &fakeLLMProvider{text: cannedHTML})

	resp, err := f.gen.Generate(f.ctx, f.generateReq())
	require.NoError(t, err)

	prompt := f.singlePrompt(t)
	// Student/class context and both instruction blocks.
	assert.Contains(t, prompt, "Student: Fenwick, Class: Geology · Mon")
	assert.Contains(t, prompt, reportSpecHeader+fixtureSpec)
	assert.Contains(t, prompt, reportInstructionsHeader+fixtureInstructions)
	// Only this student's in-range notes, each on its own dated line.
	assert.Contains(t, prompt, "- 2026-01-15: "+noteInRangeJan)
	assert.Contains(t, prompt, "- 2026-02-20: "+noteInRangeFeb)
	assert.NotContains(t, prompt, noteBeforeRange, "note dated before StartDate leaked into the prompt")
	assert.NotContains(t, prompt, noteAfterRange, "note dated after EndDate leaked into the prompt")
	assert.NotContains(t, prompt, noteClassmate, "another student's note leaked into the prompt")
	// Generate has no previous draft, so no feedback block (Regenerate test
	// asserts the positive arm).
	assert.NotContains(t, prompt, reportFeedbackHeader)

	// Response mirrors the persisted row.
	assert.Equal(t, cannedHTML, resp.HTML)
	rpt, err := f.reportRepo.GetByID(f.ctx, resp.ReportID)
	require.NoError(t, err)
	assert.Equal(t, f.studentID, rpt.StudentID)
	assert.Equal(t, fixtureStart, rpt.StartDate)
	assert.Equal(t, fixtureEnd, rpt.EndDate)
	assert.Equal(t, cannedHTML, rpt.HTML)
	require.NotNil(t, rpt.ModelVersion)
	assert.Equal(t, fakeReportModel, *rpt.ModelVersion, "row must be stamped with the report-task model")
	require.NotNil(t, rpt.PromptHash)
	assert.Equal(t, ReportPromptHash, *rpt.PromptHash)
	require.NotNil(t, rpt.Instructions)
	assert.Equal(t, fixtureInstructions, *rpt.Instructions)
	assert.NotEmpty(t, rpt.CreatedAt)
	assert.Equal(t, rpt.CreatedAt, resp.CreatedAt)

	reports, err := f.reportRepo.List(f.ctx, f.studentID)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, resp.ReportID, reports[0].ID)
}

// TestLLMReportGenerator_Generate_EmptyInstructionsStoresNull pairs with the
// test above: with no ad-hoc instructions the prompt drops the block and the
// row stores NULL rather than "".
func TestLLMReportGenerator_Generate_EmptyInstructionsStoresNull(t *testing.T) {
	f := newReportGenFixture(t, &fakeLLMProvider{text: cannedHTML})
	req := f.generateReq()
	req.Instructions = ""

	resp, err := f.gen.Generate(f.ctx, req)
	require.NoError(t, err)

	prompt := f.singlePrompt(t)
	assert.Contains(t, prompt, reportSpecHeader+fixtureSpec, "the Level spec is still there")
	assert.NotContains(t, prompt, reportInstructionsHeader)

	rpt, err := f.reportRepo.GetByID(f.ctx, resp.ReportID)
	require.NoError(t, err)
	assert.Nil(t, rpt.Instructions)
}

// TestLLMReportGenerator_Generate_NoNotesInRange pins current behaviour: a
// window with no notes is not an error — the LLM is still called with an
// empty notes list and the report is persisted.
func TestLLMReportGenerator_Generate_NoNotesInRange(t *testing.T) {
	f := newReportGenFixture(t, &fakeLLMProvider{text: cannedHTML})
	req := f.generateReq()
	// A window between the in-range notes above and the April note.
	req.StartDate = "2026-03-01"
	req.EndDate = "2026-03-31"

	resp, err := f.gen.Generate(f.ctx, req)
	require.NoError(t, err)

	prompt := f.singlePrompt(t)
	// The notes section is present but holds no bullet lines: the student
	// header is followed directly by the blank line the loop would have
	// separated bullets from, then the task footer.
	assert.Contains(t, prompt, "Student: Fenwick, Class: Geology · Mon\n\n\n"+reportTaskFooter)
	assert.NotContains(t, prompt, "- 2026-")
	assert.NotContains(t, prompt, noteInRangeJan)
	assert.NotContains(t, prompt, noteInRangeFeb)
	assert.NotContains(t, prompt, noteAfterRange)

	rpt, err := f.reportRepo.GetByID(f.ctx, resp.ReportID)
	require.NoError(t, err)
	assert.Equal(t, cannedHTML, rpt.HTML)
	assert.Equal(t, "2026-03-01", rpt.StartDate)
	assert.Equal(t, "2026-03-31", rpt.EndDate)
}

func TestLLMReportGenerator_Generate_LLMErrorLeavesNoRow(t *testing.T) {
	llmErr := errors.New("rate limited by fake provider")
	f := newReportGenFixture(t, &fakeLLMProvider{err: llmErr})

	// A pre-existing report proves the "no new row" check is looking at a
	// table that can hold rows, not an empty one by construction.
	existing := &Report{StudentID: f.studentID, StartDate: "2025-09-01", EndDate: "2025-12-20", HTML: "<p>autumn draft</p>"}
	require.NoError(t, f.reportRepo.Create(f.ctx, existing))

	resp, err := f.gen.Generate(f.ctx, f.generateReq())
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, llmErr)
	assert.Contains(t, err.Error(), "report: LLM call failed")
	assert.Contains(t, err.Error(), "rate limited by fake provider")
	f.singlePrompt(t)

	reports, err := f.reportRepo.List(f.ctx, f.studentID)
	require.NoError(t, err)
	require.Len(t, reports, 1, "only the pre-existing report; nothing inserted for the failed generation")
	assert.Equal(t, existing.ID, reports[0].ID)
}

func TestLLMReportGenerator_Regenerate_WritesNewRowKeepsOld(t *testing.T) {
	f := newReportGenFixture(t, &fakeLLMProvider{text: regeneratedHTML})

	oldInstr := "be terse"
	old := &Report{
		StudentID:    f.studentID,
		StartDate:    fixtureStart,
		EndDate:      fixtureEnd,
		HTML:         "<p>old draft</p>",
		Instructions: &oldInstr,
	}
	require.NoError(t, f.reportRepo.Create(f.ctx, old))

	resp, err := f.gen.Regenerate(f.ctx, RegenerateReportRequest{
		ReportID:           old.ID,
		Feedback:           fixtureFeedback,
		StudentID:          f.studentID,
		Student:            fixtureStudent,
		ClassName:          fixtureClassName,
		StartDate:          fixtureStart,
		EndDate:            fixtureEnd,
		UserID:             "user_abc",
		Instructions:       fixtureInstructions,
		ReportInstructions: fixtureSpec,
	})
	require.NoError(t, err)

	prompt := f.singlePrompt(t)
	assert.Contains(t, prompt, reportFeedbackHeader+fixtureFeedback)
	assert.Contains(t, prompt, "- 2026-02-20: "+noteInRangeFeb)
	assert.Contains(t, prompt, reportInstructionsHeader+fixtureInstructions)
	assert.NotContains(t, prompt, noteClassmate)

	// Current behaviour: Regenerate appends a new row rather than updating
	// old.ID in place — history is preserved, the caller gets the new id.
	assert.NotEqual(t, old.ID, resp.ReportID, "regenerate must not hand back the stale report id")
	assert.Equal(t, regeneratedHTML, resp.HTML)

	fresh, err := f.reportRepo.GetByID(f.ctx, resp.ReportID)
	require.NoError(t, err)
	assert.Equal(t, regeneratedHTML, fresh.HTML)
	assert.Equal(t, f.studentID, fresh.StudentID)
	assert.Equal(t, fixtureStart, fresh.StartDate)
	assert.Equal(t, fixtureEnd, fresh.EndDate)
	require.NotNil(t, fresh.Instructions)
	assert.Equal(t, fixtureInstructions, *fresh.Instructions, "the new row carries the request's instructions, not the old row's")
	require.NotNil(t, fresh.ModelVersion)
	assert.Equal(t, fakeReportModel, *fresh.ModelVersion)
	require.NotNil(t, fresh.PromptHash)
	assert.Equal(t, ReportPromptHash, *fresh.PromptHash)

	stale, err := f.reportRepo.GetByID(f.ctx, old.ID)
	require.NoError(t, err)
	assert.Equal(t, "<p>old draft</p>", stale.HTML, "the previous draft must survive regeneration")
	require.NotNil(t, stale.Instructions)
	assert.Equal(t, oldInstr, *stale.Instructions)

	reports, err := f.reportRepo.List(f.ctx, f.studentID)
	require.NoError(t, err)
	ids := make([]int64, 0, len(reports))
	for _, r := range reports {
		ids = append(ids, r.ID)
	}
	assert.ElementsMatch(t, []int64{old.ID, resp.ReportID}, ids)
}

func TestLLMReportGenerator_Regenerate_LLMErrorLeavesOldRowIntact(t *testing.T) {
	llmErr := errors.New("fake provider timed out")
	f := newReportGenFixture(t, &fakeLLMProvider{err: llmErr})

	old := &Report{StudentID: f.studentID, StartDate: fixtureStart, EndDate: fixtureEnd, HTML: "<p>old draft</p>"}
	require.NoError(t, f.reportRepo.Create(f.ctx, old))

	resp, err := f.gen.Regenerate(f.ctx, RegenerateReportRequest{
		ReportID:           old.ID,
		Feedback:           fixtureFeedback,
		StudentID:          f.studentID,
		Student:            fixtureStudent,
		ClassName:          fixtureClassName,
		StartDate:          fixtureStart,
		EndDate:            fixtureEnd,
		UserID:             "user_abc",
		Instructions:       fixtureInstructions,
		ReportInstructions: fixtureSpec,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, llmErr)
	assert.True(t, strings.HasPrefix(err.Error(), "report: LLM call failed: "), "got %q", err.Error())
	f.singlePrompt(t)

	stale, err := f.reportRepo.GetByID(f.ctx, old.ID)
	require.NoError(t, err)
	assert.Equal(t, "<p>old draft</p>", stale.HTML)
	assert.Nil(t, stale.Instructions)

	reports, err := f.reportRepo.List(f.ctx, f.studentID)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, old.ID, reports[0].ID)
}
