package main

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	handler "github.com/nicogaller/gradebee/backend"
)

type fakeDrafter struct {
	calls []handler.ReportInputs
}

func (f *fakeDrafter) Draft(_ context.Context, in handler.ReportInputs) (string, error) {
	f.calls = append(f.calls, in)
	return "<p>Élodie</p>", nil
}

func (f *fakeDrafter) Model() string { return "test-model" }

func seedGenReportDB(t *testing.T) *sql.DB {
	t.Helper()
	db := seedDumpDB(t)
	_, err := db.Exec(`INSERT INTO notes (id, student_id, date, summary, updated_at) VALUES
		(20, 1, '2026-08-20', 'before the break', '2026-08-20T00:00:00Z'),
		(21, 1, '2026-09-08', 'in term', '2026-09-08T00:00:00Z')`)
	require.NoError(t, err)
	return db
}

func TestGenReport_WritesReference(t *testing.T) {
	db := seedGenReportDB(t)
	gen := &fakeDrafter{}
	refs := t.TempDir()
	var out bytes.Buffer
	require.NoError(t, genReport(context.Background(), db, gen, &out, genReportOptions{
		refsDir: refs, caseID: "c", studentID: 1, start: "2026-09-01", end: "2026-09-30", instructions: "Under 300 characters.",
	}))

	require.Len(t, gen.calls, 1)
	in := gen.calls[0]
	assert.Equal(t, "Under 300 characters.", in.Instructions)
	assert.Equal(t, "Élodie", in.StudentName)
	assert.Equal(t, "Three sections.", in.ReportInstructions)
	require.Len(t, in.Notes, 1)
	assert.Equal(t, "in term", in.Notes[0].Summary)

	meta, html, err := readReference(refs, "c")
	require.NoError(t, err)
	assert.Equal(t, "<p>Élodie</p>", html)
	assert.Equal(t, int64(1), meta.StudentID)
	assert.Equal(t, "2026-09-01", meta.StartDate)
	assert.Equal(t, "2026-09-30", meta.EndDate)
	assert.Equal(t, "Under 300 characters.", meta.Instructions)
	assert.Equal(t, "test-model", meta.Model)
	assert.NotEmpty(t, meta.CreatedAt)
	assert.Equal(t, []referenceNote{{ID: 21, UpdatedAt: "2026-09-08T00:00:00Z"}}, meta.Notes)

	assert.NotContains(t, out.String(), "Élodie", "stdout must not carry the report or names")
	assert.Contains(t, out.String(), `"id": "c"`)
	assert.NotContains(t, out.String(), "report_id")
}

func TestGenReport_Refuses(t *testing.T) {
	for name, tc := range map[string]struct {
		caseID, start, end, err string
	}{
		"start before term": {"c", "2026-08-23", "2026-09-30", "before the current term"},
		"malformed start":   {"c", "2026-9-1", "2026-09-30", "start"},
		"end before start":  {"c", "2026-09-10", "2026-09-09", "before start"},
		"no notes in range": {"c", "2026-10-01", "2026-10-31", "no notes"},
		"unsafe case id":    {"../c", "2026-09-01", "2026-09-30", "must match"},
		"reference exists":  {"taken", "2026-09-01", "2026-09-30", "exists"},
	} {
		t.Run(name, func(t *testing.T) {
			db := seedGenReportDB(t)
			refs := t.TempDir()
			require.NoError(t, writeReference(refs, "taken", referenceMeta{}, ""))
			gen := &fakeDrafter{}
			err := genReport(context.Background(), db, gen, &bytes.Buffer{},
				genReportOptions{refsDir: refs, caseID: tc.caseID, studentID: 1, start: tc.start, end: tc.end})
			assert.ErrorContains(t, err, tc.err)
			assert.Empty(t, gen.calls, "drafter must not run")
		})
	}
}
