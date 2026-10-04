package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	handler "github.com/nicogaller/gradebee/backend"
)

func TestReadReportManifest_Rejects(t *testing.T) {
	cases := map[string]struct {
		manifest string
		want     string
	}{
		"no cases":        {`{"reports":[]}`, "lists no report cases"},
		"unsafe id":       {`{"reports":[{"id":"../x","description":"d","student_id":1,"report_id":1}]}`, "must match"},
		"duplicate id":    {`{"reports":[{"id":"a","description":"d","student_id":1,"report_id":1},{"id":"a","description":"d","student_id":1,"report_id":2}]}`, "duplicate id"},
		"no description":  {`{"reports":[{"id":"a","student_id":1,"report_id":1}]}`, "description is required"},
		"missing report":  {`{"reports":[{"id":"a","description":"d","student_id":1}]}`, "report_id are required"},
		"missing student": {`{"reports":[{"id":"a","description":"d","report_id":1}]}`, "report_id are required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "m.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.manifest), 0o644))
			_, err := readReportManifest(path)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestWriteReportCases(t *testing.T) {
	evals := t.TempDir()
	casesDir := filepath.Join(evals, "fixtures", "reports")
	testsPath := filepath.Join(evals, "tests.report.generated.yaml")
	stale := filepath.Join(casesDir, "dropped_case")
	require.NoError(t, os.MkdirAll(stale, 0o755))

	m := reportManifest{Reports: []reportManifestEntry{{ID: "a_case", Description: "desc", StudentID: 1, ReportID: 2}}}
	transcript := "raw transcript"
	c := reportCase{
		ReportInputs: handler.ReportInputs{
			StudentName:        "Alice",
			ClassName:          "Marcia · Mon",
			Notes:              []handler.Note{{ID: 9, Date: "2026-03-10", Summary: "read well", Transcript: &transcript}},
			ReportInstructions: "Three sections.",
			Instructions:       "be concise",
		},
		ReferenceHTML: "<p>ref</p>",
	}
	require.NoError(t, writeReportCases(m, []reportCase{c}, casesDir, testsPath))

	assert.NoDirExists(t, stale, "cases dropped from the manifest must not linger")
	notes, err := os.ReadFile(filepath.Join(casesDir, "a_case", "notes.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `[{"date":"2026-03-10","summary":"read well"}]`, string(notes))

	raw, err := os.ReadFile(testsPath)
	require.NoError(t, err)
	var tests []reportTest
	require.NoError(t, yaml.Unmarshal(raw, &tests))
	require.Len(t, tests, 1)
	assert.Equal(t, "report: desc", tests[0].Description)
	assert.Equal(t, "Alice", tests[0].Vars.StudentName)
	assert.Equal(t, "Marcia · Mon", tests[0].Vars.ClassName)
	// promptfoo resolves file:// refs against the tests file's directory.
	for ref, want := range map[string]string{
		tests[0].Vars.Notes:              string(notes),
		tests[0].Vars.ReportInstructions: "Three sections.",
		tests[0].Vars.Instructions:       "be concise",
		tests[0].Vars.Reference:          "<p>ref</p>",
	} {
		require.Regexp(t, `^file://fixtures/reports/a_case/`, ref)
		got, err := os.ReadFile(filepath.Join(evals, ref[len("file://"):]))
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
	}
}

func TestCheckReportCase(t *testing.T) {
	rpt := handler.Report{ID: 218, CreatedAt: "2026-04-03T10:00:00Z"}
	note := func(id int64, created, updated string) handler.Note {
		return handler.Note{ID: id, CreatedAt: created, UpdatedAt: updated}
	}
	cases := map[string]struct {
		notes []handler.Note
		want  string
	}{
		"same instant is not stale": {notes: []handler.Note{note(1, "2026-04-03T10:00:00.000Z", "2026-04-03T10:00:00Z")}},
		// String order would put ".5Z" before "Z".
		"created after, fractional seconds": {
			notes: []handler.Note{note(605, "2026-04-03T10:00:00.500Z", "2026-04-03T10:00:00.500Z")},
			want:  "note 605 changed after report 218 was generated; pick a newer report",
		},
		"edited after": {
			notes: []handler.Note{
				note(605, "2026-03-01T00:00:00Z", "2026-05-01T00:00:00Z"),
				note(606, "2026-03-02T00:00:00Z", "2026-03-02T00:00:00Z"),
				note(609, "2026-06-01T00:00:00Z", "2026-06-01T00:00:00Z"),
			},
			want: "notes 605, 609 changed after report 218 was generated",
		},
		"no notes": {want: "has no notes between"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkReportCase(handler.ReportInputs{Notes: tc.notes}, rpt)
			if tc.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.want)
		})
	}
}
