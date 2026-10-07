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

type fakeGenerator struct {
	calls []handler.GenerateReportRequest
}

func (f *fakeGenerator) Generate(_ context.Context, req handler.GenerateReportRequest) (*handler.GenerateReportResponse, error) {
	f.calls = append(f.calls, req)
	return &handler.GenerateReportResponse{ReportID: 99, HTML: "<p>Élodie</p>"}, nil
}

func (f *fakeGenerator) Regenerate(context.Context, handler.RegenerateReportRequest) (*handler.GenerateReportResponse, error) {
	panic("not used")
}

func seedGenReportDB(t *testing.T) *sql.DB {
	t.Helper()
	db := seedDumpDB(t)
	_, err := db.Exec(`INSERT INTO notes (student_id, date, summary) VALUES
		(1, '2026-08-29', 'before the break'), (1, '2026-09-08', 'in term')`)
	require.NoError(t, err)
	return db
}

func TestGenReport_PassesAdHocAndDBNames(t *testing.T) {
	db := seedGenReportDB(t)
	gen := &fakeGenerator{}
	var out bytes.Buffer
	require.NoError(t, genReport(context.Background(), db, gen, &out,
		genReportOptions{studentID: 1, start: "2026-09-01", end: "2026-09-30", instructions: "Under 300 characters."}))

	require.Len(t, gen.calls, 1)
	in := gen.calls[0].ReportInputs
	assert.Equal(t, "Under 300 characters.", in.Instructions)
	assert.Equal(t, "Élodie", in.StudentName)
	assert.Equal(t, "Marcia · Mon", in.ClassName)
	assert.Equal(t, "Three sections.", in.ReportInstructions)
	require.Len(t, in.Notes, 1)
	assert.Equal(t, "in term", in.Notes[0].Summary)
	assert.Equal(t, "report 99: student 1, 2026-09-01..2026-09-30, 1 notes\n", out.String())
}

func TestGenReport_Refuses(t *testing.T) {
	for name, tc := range map[string]struct {
		start, end, err string
	}{
		"start before term": {"2026-08-31", "2026-09-30", "before the current term"},
		"malformed start":   {"2026-9-1", "2026-09-30", "start"},
		"end before start":  {"2026-09-10", "2026-09-09", "before start"},
		"no notes in range": {"2026-10-01", "2026-10-31", "no notes"},
	} {
		t.Run(name, func(t *testing.T) {
			db := seedGenReportDB(t)
			gen := &fakeGenerator{}
			err := genReport(context.Background(), db, gen, &bytes.Buffer{},
				genReportOptions{studentID: 1, start: tc.start, end: tc.end})
			assert.ErrorContains(t, err, tc.err)
			assert.Empty(t, gen.calls, "generator must not run")
		})
	}
}
