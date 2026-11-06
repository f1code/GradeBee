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
	rpt := &Report{StudentID: stu.ID, TermID: testTermID(t, db, "org_a"), StartDate: "2026-03-05", EndDate: "2026-03-20", HTML: "<p>ref</p>", Instructions: &instructions}
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

// Generate picks the active Term containing the start date; archived Terms and
// dates outside every Term are refused.
func TestReportInputResolver_ForGenerate_PicksCoveringTerm(t *testing.T) {
	f := newReportInputsFixture(t)
	ctx := context.Background()
	first := testTermID(t, f.db, "org_a")
	_, err := f.db.Exec(`UPDATE terms SET start_date = '2026-01-01', end_date = '2026-06-30' WHERE id = ?`, first)
	require.NoError(t, err)
	addTerm := func(name, start, end string, archived any, text string) int64 {
		var id int64
		require.NoError(t, f.db.QueryRow(`INSERT INTO terms (group_id, name, start_date, end_date, archived_at)
			VALUES ('org_a', ?, ?, ?, ?) RETURNING id`, name, start, end, archived).Scan(&id))
		_, err := f.db.Exec(`INSERT INTO report_instructions (level_id, term_id, text) VALUES (?, ?, ?)`, f.levelID, id, text)
		require.NoError(t, err)
		return id
	}
	second := addTerm("T2", "2026-07-01", "2026-12-31", nil, "T2 text.")
	addTerm("Old", "2025-01-01", "2025-12-31", "2026-01-01", "Old text.")

	for _, tc := range []struct {
		start    string
		wantTerm int64
		wantText string
	}{
		{"2026-01-01", first, "Write three sections."},
		{"2026-06-30", first, "Write three sections."},
		{"2026-07-01", second, "T2 text."},
		{"2025-06-01", 0, ""},
		{"2027-01-01", 0, ""},
	} {
		in, err := f.resolver.ForGenerate(ctx, "org_a", ReportStudentInput{StudentID: f.studentID}, tc.start, "2027-12-31", "")
		if tc.wantTerm == 0 {
			assert.EqualError(t, err, "no term covers "+tc.start)
			continue
		}
		require.NoError(t, err, tc.start)
		assert.Equal(t, tc.wantTerm, in.TermID, tc.start)
		assert.Equal(t, tc.wantText, in.ReportInstructions, tc.start)
	}
}

// Generation sees the student's newest Report of each earlier Term of its year,
// oldest Term first; archived, current, later Terms and other students stay out.
func TestReportInputResolver_EarlierReports(t *testing.T) {
	f := newReportInputsFixture(t)
	ctx := context.Background()
	reports := &ReportRepo{db: f.db}
	t1 := testTermID(t, f.db, "org_a")
	_, err := f.db.Exec(`UPDATE terms SET start_date = '2026-01-01', end_date = '2026-03-31' WHERE id = ?`, t1)
	require.NoError(t, err)
	addTerm := func(name, start, end string, archived any) int64 {
		var id int64
		require.NoError(t, f.db.QueryRow(`INSERT INTO terms (group_id, name, start_date, end_date, archived_at)
			VALUES ('org_a', ?, ?, ?, ?) RETURNING id`, name, start, end, archived).Scan(&id))
		_, err := f.db.Exec(`INSERT INTO report_instructions (level_id, term_id, text) VALUES (?, ?, 'text')`, f.levelID, id)
		require.NoError(t, err)
		return id
	}
	t2 := addTerm("T2", "2026-04-01", "2026-06-30", nil)
	t3 := addTerm("T3", "2026-07-01", "2026-12-31", nil)
	old := addTerm("Old", "2025-01-01", "2025-12-31", "2026-01-01")
	oldFirst := addTerm("Old first", "2024-09-01", "2024-12-31", "2026-01-01")
	olderYear := addTerm("Older year", "2023-09-01", "2023-12-31", "2024-08-01")
	var classID int64
	require.NoError(t, f.db.QueryRow(`SELECT class_id FROM students WHERE id = ?`, f.studentID).Scan(&classID))
	other, err := (&StudentRepo{db: f.db}).Create(ctx, classID, "Bob")
	require.NoError(t, err)
	otherGroup := testTermID(t, f.db, "org_b")
	// The fixture's "<p>ref</p>" is the older t1 Report.
	for _, r := range []Report{
		{StudentID: f.studentID, TermID: t1, HTML: "<p>t1 newest</p>"},
		{StudentID: f.studentID, TermID: otherGroup, HTML: "<p>other group</p>"},
		{StudentID: f.studentID, TermID: t2, HTML: "<p>t2</p>"},
		{StudentID: f.studentID, TermID: t3, HTML: "<p>t3 own term</p>"},
		{StudentID: f.studentID, TermID: old, HTML: "<p>last year</p>"},
		{StudentID: f.studentID, TermID: oldFirst, HTML: "<p>last year first</p>"},
		{StudentID: f.studentID, TermID: olderYear, HTML: "<p>older year</p>"},
		{StudentID: other.ID, TermID: t1, HTML: "<p>bob</p>"},
	} {
		r.StartDate, r.EndDate = "2026-01-01", "2026-01-02"
		require.NoError(t, reports.Create(ctx, &r))
	}
	var lastYearID int64
	require.NoError(t, f.db.QueryRow(`SELECT id FROM reports WHERE html = '<p>last year</p>'`).Scan(&lastYearID))

	in, err := f.resolver.ForGenerate(ctx, "org_a", ReportStudentInput{StudentID: f.studentID}, "2026-07-01", "2026-12-31", "")
	require.NoError(t, err)
	assert.Equal(t, []EarlierReport{{"Test Term", "<p>t1 newest</p>"}, {"T2", "<p>t2</p>"}}, in.EarlierReports)

	in, err = f.resolver.ForGenerate(ctx, "org_a", ReportStudentInput{StudentID: f.studentID}, "2026-01-01", "2026-03-31", "")
	require.NoError(t, err)
	assert.Empty(t, in.EarlierReports, "first term")

	// A regenerate after rollover reads its own archived year only.
	in, _, err = f.resolver.ForReport(ctx, "org_a", f.studentID, lastYearID)
	require.NoError(t, err)
	assert.Equal(t, []EarlierReport{{"Old first", "<p>last year first</p>"}}, in.EarlierReports)
}
