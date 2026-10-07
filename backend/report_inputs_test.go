package handler

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reportInputsFixture struct {
	db        *sql.DB
	resolver  *ReportInputResolver
	levelID   int64
	studentID int64
	reportID  int64
}

// newReportInputsFixture seeds one student with notes inside and outside a
// report's range, under a Level in org_a that has Report Instructions.
func newReportInputsFixture(t *testing.T) reportInputsFixture {
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
	return reportInputsFixture{db: db, resolver: NewReportInputResolver(db), levelID: cls.LevelID, studentID: stu.ID, reportID: rpt.ID}
}

func TestReportInputResolver_ForReport(t *testing.T) {
	f := newReportInputsFixture(t)

	for _, groupID := range []string{"org_a", ""} {
		in, rpt, err := f.resolver.ForReport(context.Background(), groupID, f.studentID, f.reportID)
		require.NoError(t, err, "group %q", groupID)

		assert.Equal(t, f.studentID, in.StudentID)
		assert.Equal(t, "Alice", in.StudentName)
		assert.Equal(t, "Marcia · Mon · 17:25", in.ClassName)
		assert.Equal(t, "Marcia", in.LevelName)
		assert.Equal(t, "Write three sections.", in.ReportInstructions)
		assert.Equal(t, "be concise", in.Instructions)
		assert.Equal(t, "2026-03-05", in.StartDate)
		assert.Equal(t, "2026-03-20", in.EndDate)
		require.Len(t, in.Notes, 1)
		assert.Equal(t, "in range", in.Notes[0].Summary)
		assert.Equal(t, f.reportID, rpt.ID)
		assert.Equal(t, "<p>ref</p>", rpt.HTML)
		assert.NotEmpty(t, rpt.CreatedAt)
	}
}

func TestReportInputResolver_ForReport_NullInstructions(t *testing.T) {
	f := newReportInputsFixture(t)
	_, err := f.db.Exec("UPDATE reports SET instructions = NULL")
	require.NoError(t, err)

	in, _, err := f.resolver.ForReport(context.Background(), "org_a", f.studentID, f.reportID)
	require.NoError(t, err)
	assert.Empty(t, in.Instructions)
}

func TestReportInputResolver_ForGenerate(t *testing.T) {
	f := newReportInputsFixture(t)

	in, err := f.resolver.ForGenerate(context.Background(), "org_a",
		ReportStudentInput{StudentID: f.studentID, Name: "Alice (request)", ClassName: "Class (request)"},
		"2026-02-01", "2026-03-05", "ad hoc")
	require.NoError(t, err)

	assert.Equal(t, f.studentID, in.StudentID)
	assert.Equal(t, "Alice (request)", in.StudentName, "names come from the request")
	assert.Equal(t, "Class (request)", in.ClassName)
	assert.Equal(t, "Marcia", in.LevelName)
	assert.Equal(t, "Write three sections.", in.ReportInstructions)
	assert.Equal(t, "ad hoc", in.Instructions)
	assert.Equal(t, "2026-02-01", in.StartDate)
	assert.Equal(t, "2026-03-05", in.EndDate)
	require.Len(t, in.Notes, 1)
	assert.Equal(t, "early", in.Notes[0].Summary)
}

func TestReportInputResolver_ForGenerate_BlankNamesFromDB(t *testing.T) {
	f := newReportInputsFixture(t)

	in, err := f.resolver.ForGenerate(context.Background(), "", ReportStudentInput{StudentID: f.studentID}, "2026-02-01", "2026-03-05", "")
	require.NoError(t, err)
	assert.Equal(t, "Alice", in.StudentName)
	assert.Equal(t, "Marcia · Mon · 17:25", in.ClassName)
}

func TestReportInputResolver_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown report", func(t *testing.T) {
		f := newReportInputsFixture(t)
		_, _, err := f.resolver.ForReport(ctx, "", f.studentID, f.reportID+1)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("report of another student", func(t *testing.T) {
		f := newReportInputsFixture(t)
		_, _, err := f.resolver.ForReport(ctx, "", f.studentID+1, f.reportID)
		assert.ErrorContains(t, err, "belongs to student")
	})

	t.Run("unknown student", func(t *testing.T) {
		f := newReportInputsFixture(t)
		_, err := f.resolver.ForGenerate(ctx, "org_a", ReportStudentInput{StudentID: f.studentID + 1}, "2026-01-01", "2026-12-31", "")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.ErrorContains(t, err, "student")
	})

	t.Run("Level outside the request's Group", func(t *testing.T) {
		f := newReportInputsFixture(t)
		_, _, err := f.resolver.ForReport(ctx, "org_b", f.studentID, f.reportID)
		assert.ErrorIs(t, err, ErrNotFound)
		assert.ErrorContains(t, err, "level")

		_, err = f.resolver.ForGenerate(ctx, "org_b", ReportStudentInput{StudentID: f.studentID}, "2026-01-01", "2026-12-31", "")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.ErrorContains(t, err, "level")
	})

	t.Run("Level without Report Instructions", func(t *testing.T) {
		f := newReportInputsFixture(t)
		require.NoError(t, (&LevelRepo{db: f.db}).UpdateReportInstructions(ctx, "org_a", f.levelID, "  \n"))

		_, _, err := f.resolver.ForReport(ctx, "", f.studentID, f.reportID)
		var missing *ErrLevelInstructionsMissing
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "Marcia", missing.LevelName)
		assert.EqualError(t, err, "Level 'Marcia' has no report instructions — an admin must set them up")

		_, err = f.resolver.ForGenerate(ctx, "org_a", ReportStudentInput{StudentID: f.studentID}, "2026-01-01", "2026-12-31", "")
		require.ErrorAs(t, err, &missing)
	})
}
