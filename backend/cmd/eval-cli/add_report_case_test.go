package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	handler "github.com/nicogaller/gradebee/backend"
)

// seedAddReportDB gives student 1 a usable report 10 (newest), a report 11
// with no notes in range, and an older usable report 12.
func seedAddReportDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := handler.OpenDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, handler.RunMigrations(db))
	for _, stmt := range []string{
		`INSERT INTO levels (id, group_id, name, report_instructions) VALUES (1, 'g', 'Marcia', 'Three sections.')`,
		`INSERT INTO classes (id, user_id, level_id, day) VALUES (1, 'u', 1, 'Monday')`,
		`INSERT INTO students (id, class_id, name) VALUES (1, 1, 'Alice')`,
		`INSERT INTO notes (student_id, date, summary) VALUES (1, '2026-03-10', 'a'), (1, '2026-03-11', 'b')`,
		`INSERT INTO reports (id, student_id, start_date, end_date, html, instructions, created_at) VALUES
			(10, 1, '2026-03-01', '2026-03-31', '<p/>', 'be concise', '2026-04-03T00:00:00Z'),
			(11, 1, '2026-05-01', '2026-05-31', '<p/>', NULL, '2026-04-02T00:00:00Z'),
			(12, 1, '2026-03-01', '2026-03-10', '<p/>', NULL, '2026-04-01T00:00:00Z')`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	return db
}

func TestAddReportCase_DraftsNewestUsableReport(t *testing.T) {
	db := seedAddReportDB(t)
	var out bytes.Buffer
	require.NoError(t, addReportCase(context.Background(), db, &out, 1, 0, reportManifest{}))

	lines := strings.Split(out.String(), "\n")
	assert.Regexp(t, `^10 +2026-03-01\.\.2026-03-31 `, lines[1])
	assert.Regexp(t, `Marcia +yes +yes +2$`, lines[1])
	assert.Regexp(t, `^11 .* no +0$`, lines[2])
	assert.Contains(t, lines[3], "12 ")
	assert.Contains(t, out.String(), `"description": "Level Report Instructions, 2 notes, ad-hoc instructions"`)
	assertDraftParses(t, out.String(), reportManifestEntry{ID: "student1_report10", StudentID: 1, ReportID: 10})
	assert.NotContains(t, draftOf(out.String()), "Alice")
	assert.NotContains(t, draftOf(out.String()), "Marcia")
}

func TestAddReportCase_SkipsReportsInManifest(t *testing.T) {
	db := seedAddReportDB(t)
	existing := reportManifest{Reports: []reportManifestEntry{{ID: "x", ReportID: 10}}}
	var out bytes.Buffer
	require.NoError(t, addReportCase(context.Background(), db, &out, 1, 0, existing))
	assertDraftParses(t, out.String(), reportManifestEntry{ID: "student1_report12", StudentID: 1, ReportID: 12})
}

func TestAddReportCase_ExplicitReport(t *testing.T) {
	db := seedAddReportDB(t)
	var out bytes.Buffer
	require.NoError(t, addReportCase(context.Background(), db, &out, 1, 12, reportManifest{}))
	assert.Contains(t, out.String(), "1 notes, no ad-hoc instructions")
	assertDraftParses(t, out.String(), reportManifestEntry{ID: "student1_report12", StudentID: 1, ReportID: 12})
}

func TestAddReportCase_Errors(t *testing.T) {
	cases := map[string]struct {
		studentID, reportID int64
		existing            reportManifest
		want                string
	}{
		"unknown student":    {studentID: 2, want: "student 2 not found"},
		"report in manifest": {studentID: 1, reportID: 10, existing: reportManifest{Reports: []reportManifestEntry{{ID: "x", ReportID: 10}}}, want: `already in the manifest as "x"`},
		"no notes in range":  {studentID: 1, reportID: 11, want: "no notes between"},
		"unknown report":     {studentID: 1, reportID: 99, want: "not found"},
		"none usable": {studentID: 1, existing: reportManifest{Reports: []reportManifestEntry{
			{ID: "a", ReportID: 10}, {ID: "b", ReportID: 12},
		}}, want: "has no report outside the manifest"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := seedAddReportDB(t)
			err := addReportCase(context.Background(), db, &bytes.Buffer{}, tc.studentID, tc.reportID, tc.existing)
			assert.ErrorContains(t, err, tc.want)
		})
	}

	t.Run("student without reports", func(t *testing.T) {
		db := seedAddReportDB(t)
		_, err := db.Exec("DELETE FROM reports")
		require.NoError(t, err)
		err = addReportCase(context.Background(), db, &bytes.Buffer{}, 1, 0, reportManifest{})
		assert.ErrorContains(t, err, "has no reports")
	})
}

func draftOf(out string) string {
	_, draft, _ := strings.Cut(out, "\n    {")
	return "{" + draft
}

// assertDraftParses pastes the draft into a manifest and reads it back the
// way gen-report-cases does.
func assertDraftParses(t *testing.T, out string, want reportManifestEntry) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"reports":[`+draftOf(out)+`]}`), 0o644))
	m, err := readReportManifest(path)
	require.NoError(t, err)
	require.Len(t, m.Reports, 1)
	got := m.Reports[0]
	assert.NotEmpty(t, got.Description)
	got.Description = ""
	assert.Equal(t, want, got)
}
