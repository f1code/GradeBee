package handler

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reportEvalFixture struct {
	db        *sql.DB
	levelID   int64
	studentID int64
	reportID  int64
}

// newReportEvalFixture seeds one student with notes inside and outside a
// report's range, under a Level that has Report Instructions.
func newReportEvalFixture(t *testing.T) reportEvalFixture {
	t.Helper()
	ctx := context.Background()
	db := setupTestDB(t)
	cls := newTestClass(t, &ClassRepo{db: db}, "org_a", "user_a", "Marcia", "17:25")
	require.NoError(t, (&LevelRepo{db: db}).UpdateReportInstructions(ctx, "org_a", cls.LevelID, "Write three sections."))
	stu, err := (&StudentRepo{db: db}).Create(ctx, cls.ID, "Alice")
	require.NoError(t, err)
	notes := &NoteRepo{db: db}
	for _, n := range []Note{
		{StudentID: stu.ID, Date: "2026-03-01", Summary: "early", Source: "manual"},
		{StudentID: stu.ID, Date: "2026-03-10", Summary: "in range", Source: "manual"},
		{StudentID: stu.ID, Date: "2026-04-01", Summary: "late", Source: "manual"},
	} {
		require.NoError(t, notes.Create(ctx, &n))
	}
	instructions := "be concise"
	rpt := &Report{StudentID: stu.ID, StartDate: "2026-03-05", EndDate: "2026-03-20", HTML: "<p>ref</p>", Instructions: &instructions}
	require.NoError(t, (&ReportRepo{db: db}).Create(ctx, rpt))
	return reportEvalFixture{db: db, levelID: cls.LevelID, studentID: stu.ID, reportID: rpt.ID}
}

func TestLoadReportEvalCase_MatchesRegenerateInputs(t *testing.T) {
	f := newReportEvalFixture(t)

	c, err := LoadReportEvalCase(context.Background(), f.db, f.studentID, f.reportID)
	require.NoError(t, err)

	assert.Equal(t, "Alice", c.StudentName)
	assert.Equal(t, "Marcia · Mon · 17:25", c.ClassName)
	assert.Equal(t, "Write three sections.", c.ReportInstructions)
	assert.Equal(t, "be concise", c.Instructions)
	assert.Equal(t, "<p>ref</p>", c.ReferenceHTML)
	require.Len(t, c.Notes, 1)
	assert.Equal(t, "in range", c.Notes[0].Summary)
}

func TestLoadReportEvalCase_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown report", func(t *testing.T) {
		f := newReportEvalFixture(t)
		_, err := LoadReportEvalCase(ctx, f.db, f.studentID, f.reportID+1)
		assert.ErrorContains(t, err, "not found")
	})

	t.Run("report of another student", func(t *testing.T) {
		f := newReportEvalFixture(t)
		_, err := LoadReportEvalCase(ctx, f.db, f.studentID+1, f.reportID)
		assert.ErrorContains(t, err, "belongs to student")
	})

	t.Run("no notes in range", func(t *testing.T) {
		f := newReportEvalFixture(t)
		_, err := f.db.Exec("DELETE FROM notes WHERE summary = 'in range'")
		require.NoError(t, err)
		_, err = LoadReportEvalCase(ctx, f.db, f.studentID, f.reportID)
		assert.ErrorContains(t, err, "no notes between 2026-03-05 and 2026-03-20")
	})

	t.Run("Level without Report Instructions", func(t *testing.T) {
		f := newReportEvalFixture(t)
		require.NoError(t, (&LevelRepo{db: f.db}).UpdateReportInstructions(ctx, "org_a", f.levelID, "  \n"))
		_, err := LoadReportEvalCase(ctx, f.db, f.studentID, f.reportID)
		assert.ErrorContains(t, err, "has no Report Instructions")
	})
}

func TestListReportEvalCandidates(t *testing.T) {
	ctx := context.Background()
	f := newReportEvalFixture(t)
	older := &Report{StudentID: f.studentID, StartDate: "2026-05-01", EndDate: "2026-05-31", HTML: "<p>old</p>", Instructions: new(string)}
	require.NoError(t, (&ReportRepo{db: f.db}).Create(ctx, older))
	_, err := f.db.Exec("UPDATE reports SET created_at = '2020-01-01T00:00:00Z' WHERE id = ?", older.ID)
	require.NoError(t, err)

	got, err := ListReportEvalCandidates(ctx, f.db, f.studentID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, f.reportID, got[0].ReportID, "newest first")
	assert.Equal(t, "2026-03-05", got[0].StartDate)
	assert.Equal(t, "2026-03-20", got[0].EndDate)
	assert.True(t, got[0].HasReportInstructions)
	assert.True(t, got[0].HasInstructions)
	assert.Equal(t, 1, got[0].NoteCount)
	assert.Equal(t, older.ID, got[1].ReportID)
	assert.False(t, got[1].HasInstructions, "empty ad-hoc instructions")
	assert.Equal(t, 0, got[1].NoteCount)

	require.NoError(t, (&LevelRepo{db: f.db}).UpdateReportInstructions(ctx, "org_a", f.levelID, "  \n"))
	got, err = ListReportEvalCandidates(ctx, f.db, f.studentID)
	require.NoError(t, err)
	assert.False(t, got[0].HasReportInstructions, "blank Report Instructions")
}

func TestListReportEvalCandidates_Errors(t *testing.T) {
	ctx := context.Background()
	f := newReportEvalFixture(t)

	_, err := ListReportEvalCandidates(ctx, f.db, f.studentID+1)
	assert.ErrorContains(t, err, "not found")

	_, err = f.db.Exec("DELETE FROM reports")
	require.NoError(t, err)
	_, err = ListReportEvalCandidates(ctx, f.db, f.studentID)
	assert.ErrorContains(t, err, "has no reports")
}
