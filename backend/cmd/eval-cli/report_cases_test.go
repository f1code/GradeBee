package main

import (
	"bytes"
	"context"
	"database/sql"
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
		"no cases":                 {`{"reports":[]}`, "lists no report cases"},
		"unsafe id":                {`{"reports":[{"id":"../x","description":"d","student_id":1}]}`, "must match"},
		"duplicate id":             {`{"reports":[{"id":"a","description":"d","student_id":1},{"id":"a","description":"d","student_id":2}]}`, "duplicate id"},
		"no description":           {`{"reports":[{"id":"a","student_id":1}]}`, "description is required"},
		"missing student":          {`{"reports":[{"id":"a","description":"d"}]}`, "student_id is required"},
		"fresh with report_id":     {`{"reports":[{"id":"a","description":"d","student_id":1,"report_id":1}]}`, "not report_id"},
		"regenerate, no report_id": {`{"reports":[{"id":"a","description":"d","student_id":1,"feedback":[1]}]}`, "needs report_id"},
		"tweak, no ask":            {`{"reports":[{"id":"a","description":"d","student_id":1,"tweak":{"of":"b","paragraph":0}}]}`, "needs of and ask"},
		"tweak, one-sided chars":   {`{"reports":[{"id":"a","description":"d","student_id":1,"tweak":{"of":"b","ask":"x","chars":[1]}}]}`, "[min, max]"},
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

	m := reportManifest{Reports: []reportManifestEntry{{ID: "a_case", Description: "desc", StudentID: 1, Expect: []string{"no cushions"}}}}
	transcript := "raw transcript"
	c := reportCase{
		ReportInputs: handler.ReportInputs{
			StudentName:        "Alice",
			ClassName:          "Marcia · Mon",
			Notes:              []handler.Note{{ID: 9, Date: "2026-03-10", Summary: "read well", Transcript: &transcript}},
			ReportInstructions: "Three sections.",
			Instructions:       "be concise",
			EarlierReports:     []handler.EarlierReport{{TermName: "T1", HTML: "<p>t1</p>"}},
		},
		ReferenceHTML: "<p>ref</p>",
	}
	require.NoError(t, writeReportCases(m, []reportCase{c}, casesDir, testsPath, experiment{}))

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
	earlier, err := os.ReadFile(filepath.Join(evals, tests[0].Vars.EarlierReports[len("file://"):]))
	require.NoError(t, err)
	assert.JSONEq(t, `[{"termName":"T1","html":"<p>t1</p>"}]`, string(earlier))
	assert.Empty(t, tests[0].Prompts, "a fresh case runs on every row, gold-reference included")
	require.Len(t, tests[0].Assert, 1)
	assert.Equal(t, "expect", tests[0].Assert[0].Metric)
	assert.Contains(t, tests[0].Assert[0].Value, "no cushions")
}

func TestWriteReportCases_Regenerate(t *testing.T) {
	evals := t.TempDir()
	casesDir := filepath.Join(evals, "fixtures", "reports")
	testsPath := filepath.Join(evals, "tests.report.generated.yaml")
	m := reportManifest{Reports: []reportManifestEntry{
		{ID: "regen", Description: "desc", StudentID: 1, ReportID: 2, Feedback: []int64{81, 82}, Expect: []string{"no cushions"}},
		{ID: "regen_plain", Description: "plain", StudentID: 1, ReportID: 3, Feedback: []int64{83}},
	}}
	notes := []handler.Note{
		{ID: 1, Date: "2026-03-10", Summary: "read well", UpdatedAt: "2026-03-10T00:00:00.000Z"},
		{ID: 2, Date: "2026-03-12", Summary: "now says hello", UpdatedAt: "2026-04-05T00:00:00.000Z"},
	}
	c := reportCase{
		ReportInputs:   handler.ReportInputs{StudentName: "Alice", Notes: notes},
		ReferenceHTML:  "<p>draft</p>",
		Feedback:       []string{"take out the cushions", "warmer"},
		ChangedNotes:   notes[1:],
		DraftCreatedAt: "2026-04-01T00:00:00.000Z",
	}
	plain := reportCase{ReportInputs: handler.ReportInputs{Notes: notes}, ReferenceHTML: "<p/>", Feedback: []string{"shorter"}}
	require.NoError(t, writeReportCases(m, []reportCase{c, plain}, casesDir, testsPath, experiment{}))

	raw, err := os.ReadFile(testsPath)
	require.NoError(t, err)
	var tests []reportTest
	require.NoError(t, yaml.Unmarshal(raw, &tests))
	require.Len(t, tests, 2)
	tc := tests[0]
	assert.Equal(t, []string{"gradebee-prompt"}, tc.Prompts, "the gold-reference row would echo the unrevised draft")
	assert.Equal(t, tc.Vars.Reference, tc.Vars.Draft, "the judge's reference is the draft the model revises")
	fb, err := os.ReadFile(filepath.Join(evals, tc.Vars.Feedback[len("file://"):]))
	require.NoError(t, err)
	assert.JSONEq(t, `["take out the cushions","warmer"]`, string(fb), "rounds oldest first")
	assert.Equal(t, "2026-04-01T00:00:00.000Z", tc.Vars.DraftCreatedAt)

	notesOf := func(tc reportTest) string {
		raw, err := os.ReadFile(filepath.Join(evals, tc.Vars.Notes[len("file://"):]))
		require.NoError(t, err)
		return string(raw)
	}
	assert.JSONEq(t, `[{"date":"2026-03-10","summary":"read well"},
		{"date":"2026-03-12","summary":"now says hello","updatedAt":"2026-04-05T00:00:00.000Z"}]`,
		notesOf(tc), "only the changed note keeps updatedAt, so round 1 tags it alone")
	assert.NotContains(t, notesOf(tests[1]), "updatedAt", "no notes changed since that draft")

	metricsOf := func(tc reportTest) []string {
		var out []string
		for _, a := range tc.Assert {
			out = append(out, a.Metric)
		}
		return out
	}
	assert.Equal(t, []string{"feedback", "feedback", "kept", "new-notes", "expect"}, metricsOf(tc))
	assert.Contains(t, tc.Assert[0].Value, `Round 1 asked: "take out the cushions"`)
	assert.Contains(t, tc.Assert[1].Value, `Round 2 asked: "warmer"`)
	assert.Contains(t, tc.Assert[3].Value, "- 2026-03-12: now says hello")
	assert.Contains(t, tc.Assert[4].Value, "no cushions")
	assert.Equal(t, []string{"feedback", "kept"}, metricsOf(tests[1]), "no notes changed since the draft, no new-notes line")
}

// A tweak case outputs one paragraph: the whole-report default axes are off,
// and the judge reads the paragraph it rewrites.
func TestWriteReportCases_Tweak(t *testing.T) {
	evals := t.TempDir()
	casesDir := filepath.Join(evals, "fixtures", "reports")
	testsPath := filepath.Join(evals, "tests.report.generated.yaml")
	tw := reportTweak{Of: "base", Paragraph: 1, Ask: "pad to 300 characters", Chars: []int{250, 350}}
	m := reportManifest{Reports: []reportManifestEntry{{ID: "tweak", Description: "desc", StudentID: 1, Tweak: &tw}}}
	c := reportCase{
		ReportInputs:  handler.ReportInputs{StudentName: "Alice"},
		ReferenceHTML: "<p>one</p><p>two [MISSING]</p>",
		Feedback:      []string{tw.Ask},
		ParagraphText: "two {{x}} [MISSING]",
	}
	require.NoError(t, writeReportCases(m, []reportCase{c}, casesDir, testsPath, experiment{}))

	raw, err := os.ReadFile(testsPath)
	require.NoError(t, err)
	var tests []reportTest
	require.NoError(t, yaml.Unmarshal(raw, &tests))
	require.Len(t, tests, 1)
	tc := tests[0]
	assert.Equal(t, []string{"gradebee-prompt"}, tc.Prompts)
	require.NotNil(t, tc.Options)
	assert.True(t, tc.Options.DisableDefaultAsserts)
	require.NotNil(t, tc.Vars.Paragraph)
	assert.Equal(t, 1, *tc.Vars.Paragraph)
	assert.Equal(t, tc.Vars.Reference, tc.Vars.Draft)
	fb, err := os.ReadFile(filepath.Join(evals, tc.Vars.Feedback[len("file://"):]))
	require.NoError(t, err)
	assert.JSONEq(t, `["pad to 300 characters"]`, string(fb))

	var metrics []string
	for _, a := range tc.Assert {
		metrics = append(metrics, a.Metric)
	}
	assert.Equal(t, []string{"one-paragraph", "length", "tweak", "no-invention"}, metrics)
	assert.Contains(t, tc.Assert[1].Value, "t.length >= 250 && t.length <= 350")
	assert.Contains(t, tc.Assert[2].Value, `"two x [MISSING]"`, "braces stripped for nunjucks")
}

func TestLoadFeedbackComment(t *testing.T) {
	db := seedDumpDB(t)
	for _, stmt := range []string{
		`INSERT INTO reports (id, student_id, term_id, start_date, end_date, html, created_at) VALUES (20, 2, 1, '2026-03-01', '2026-03-31', '<p/>', '2026-04-03T00:00:00Z')`,
		`INSERT INTO artifact_feedback (id, artifact_type, artifact_id, rating, signal, comment, user_id) VALUES
			(1, 'report', 10, 'down', 'regenerated', 'warmer', 'u'),
			(2, 'report', 20, 'down', 'regenerated', 'shorter', 'u'),
			(3, 'report', 10, 'down', 'explicit', NULL, 'u'),
			(4, 'note', 10, 'down', 'edited', 'x', 'u')`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	ctx := context.Background()
	got, err := loadFeedbackComment(ctx, db, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "warmer", got)

	for id, want := range map[int64]string{
		2: "on a report of student 2, not student 1",
		3: "has no comment",
		4: "no feedback on a report",
		9: "no feedback on a report",
	} {
		_, err := loadFeedbackComment(ctx, db, 1, id)
		assert.ErrorContains(t, err, want, "feedback %d", id)
	}
}

func TestNotesChangedSince(t *testing.T) {
	rpt := handler.Report{ID: 218, CreatedAt: "2026-04-03T10:00:00Z"}
	note := func(id int64, created, updated string) handler.Note {
		return handler.Note{ID: id, CreatedAt: created, UpdatedAt: updated}
	}
	changed, err := notesChangedSince(handler.ReportInputs{Notes: []handler.Note{
		note(1, "2026-04-03T10:00:00.000Z", "2026-04-03T10:00:00Z"), // same instant
		// String order would put ".5Z" before "Z".
		note(605, "2026-04-03T10:00:00.500Z", "2026-04-03T10:00:00.500Z"),
		note(606, "2026-03-01T00:00:00Z", "2026-05-01T00:00:00Z"), // edited after
		note(607, "2026-03-02T00:00:00Z", "2026-03-02T00:00:00Z"),
	}}, rpt)
	require.NoError(t, err)
	ids := make([]int64, len(changed))
	for i, n := range changed {
		ids[i] = n.ID
	}
	assert.Equal(t, []int64{605, 606}, ids)
}

// seedReferenceStore writes a reference for case "c" from student 1's notes as
// seedDumpDB leaves them, and returns the DB and store.
func seedReferenceStore(t *testing.T) (db *sql.DB, refs string) {
	t.Helper()
	db = seedDumpDB(t)
	in, err := handler.NewReportInputResolver(db).ForGenerate(context.Background(), "", handler.ReportStudentInput{StudentID: 1}, "2026-03-01", "2026-03-31", "be brief")
	require.NoError(t, err)
	require.Len(t, in.Notes, 2)
	refs = t.TempDir()
	require.NoError(t, writeReference(refs, "c", referenceMeta{
		StudentID: 1, StartDate: "2026-03-01", EndDate: "2026-03-31", Instructions: "be brief",
		LevelInstructions: in.ReportInstructions, Notes: referenceNotes(in.Notes),
	}, "<p>ref</p>"))
	return db, refs
}

func TestLoadFreshCase(t *testing.T) {
	db, refs := seedReferenceStore(t)
	// A prod refresh drops local report rows; the case must not need them.
	_, err := db.Exec(`DELETE FROM reports`)
	require.NoError(t, err)
	c, err := loadFreshCase(context.Background(), handler.NewReportInputResolver(db), refs,
		reportManifestEntry{ID: "c", Description: "d", StudentID: 1})
	require.NoError(t, err)
	assert.Equal(t, "<p>ref</p>", c.ReferenceHTML)
	assert.Equal(t, "be brief", c.Instructions)
	assert.Equal(t, "Élodie", c.StudentName)
	assert.Len(t, c.Notes, 2)
	assert.False(t, c.LevelDrift)
}

// setLevelSnapshot rewrites case "c"'s reference with another Level text.
func setLevelSnapshot(t *testing.T, refs, text string) {
	t.Helper()
	meta, html, err := readReference(refs, "c")
	require.NoError(t, err)
	meta.LevelInstructions = text
	require.NoError(t, os.RemoveAll(filepath.Join(refs, "c")))
	require.NoError(t, writeReference(refs, "c", meta, html))
}

func TestLoadFreshCase_LevelSnapshot(t *testing.T) {
	db, refs := seedReferenceStore(t)
	_, err := db.Exec(`UPDATE report_instructions SET text = 'Edited since.'`)
	require.NoError(t, err)
	resolver := handler.NewReportInputResolver(db)
	e := reportManifestEntry{ID: "c", Description: "d", StudentID: 1}

	c, err := loadFreshCase(context.Background(), resolver, refs, e)
	require.NoError(t, err)
	assert.Equal(t, "Three sections.", c.ReportInstructions, "a Level edit must not change the case's inputs")
	assert.True(t, c.LevelDrift)

	setLevelSnapshot(t, refs, "")
	_, err = loadFreshCase(context.Background(), resolver, refs, e)
	assert.ErrorContains(t, err, "no level_instructions")
}

func TestLoadRegenerateCase_LevelSnapshot(t *testing.T) {
	db := seedDumpDB(t)
	_, err := db.Exec(`INSERT INTO artifact_feedback (id, artifact_type, artifact_id, rating, signal, comment, user_id) VALUES (1, 'report', 10, 'down', 'regenerated', 'warmer', 'u')`)
	require.NoError(t, err)
	resolver := handler.NewReportInputResolver(db)
	e := reportManifestEntry{ID: "r", Description: "d", StudentID: 1, ReportID: 10, Feedback: []int64{1}}

	_, err = loadRegenerateCase(context.Background(), db, resolver, e)
	assert.ErrorContains(t, err, "report 10 has no level_instructions")

	_, err = db.Exec(`UPDATE reports SET level_instructions = 'Snapshot.' WHERE id = 10`)
	require.NoError(t, err)
	c, err := loadRegenerateCase(context.Background(), db, resolver, e)
	require.NoError(t, err)
	assert.Equal(t, "Snapshot.", c.ReportInstructions)
	assert.True(t, c.LevelDrift)
}

func TestReadCandidates(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "map.json")
	require.NoError(t, os.WriteFile(mapPath, []byte(`{"linda.md": ["Linda", "Marcia"], "sam.md": ["Sam"]}`), 0o644))
	cand := filepath.Join(dir, "cand")
	require.NoError(t, os.MkdirAll(cand, 0o755))

	_, err := readCandidates(mapPath, cand)
	assert.ErrorContains(t, err, "holds none of the files")

	require.NoError(t, os.WriteFile(filepath.Join(cand, "linda.md"), []byte("\nNew text.\n"), 0o644))
	got, err := readCandidates(mapPath, cand)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Linda": "New text.", "Marcia": "New text."}, got)

	require.NoError(t, os.WriteFile(filepath.Join(cand, "sam.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(mapPath, []byte(`{"linda.md": ["Linda"], "sam.md": ["Linda"]}`), 0o644))
	_, err = readCandidates(mapPath, cand)
	assert.ErrorContains(t, err, `Level "Linda" under two candidate files`)
}

func TestPrintCandidateHeader(t *testing.T) {
	m := reportManifest{Reports: []reportManifestEntry{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
	cases := []reportCase{
		{ReportInputs: handler.ReportInputs{LevelName: "Linda"}},
		{ReportInputs: handler.ReportInputs{LevelName: "Sam"}},
		{ReportInputs: handler.ReportInputs{LevelName: "Linda"}},
	}
	var out bytes.Buffer
	printCandidateHeader(&out, m, cases, map[string]string{"Linda": "x"})
	assert.Equal(t, "candidate text (2): a, c\nsnapshot text, no candidate for the Level (1): b\n", out.String())
}

func TestWriteReportCases_Experiment(t *testing.T) {
	evals := t.TempDir()
	casesDir := filepath.Join(evals, "fixtures", "reports")
	testsPath := filepath.Join(evals, "tests.report.experiment.generated.yaml")
	tw := reportTweak{Of: "fresh", Ask: "longer", StripLevelExamples: true}
	m := reportManifest{Reports: []reportManifestEntry{
		{ID: "fresh", Description: "fresh", StudentID: 1},
		{ID: "other_level", Description: "other", StudentID: 2},
		{ID: "regen", Description: "regen", StudentID: 1, ReportID: 3, Feedback: []int64{1}},
		{ID: "tweak", Description: "tweak", StudentID: 1, Tweak: &tw},
	}}
	in := func(level string) handler.ReportInputs {
		return handler.ReportInputs{LevelName: level, ReportInstructions: "Snapshot."}
	}
	cases := []reportCase{
		{ReportInputs: in("Linda"), ReferenceHTML: "<p/>"},
		{ReportInputs: in("Sam"), ReferenceHTML: "<p/>"},
		{ReportInputs: in("Linda"), ReferenceHTML: "<p/>", Feedback: []string{"warmer"}},
		{ReportInputs: in("Linda"), ReferenceHTML: "<p>x</p>", Feedback: []string{"longer"}},
	}
	x := experiment{noReference: true, candidates: map[string]string{"Linda": `Say "Reads with growing confidence." now.`}}
	require.NoError(t, writeReportCases(m, cases, casesDir, testsPath, x))

	raw, err := os.ReadFile(testsPath)
	require.NoError(t, err)
	var tests []reportTest
	require.NoError(t, yaml.Unmarshal(raw, &tests))
	require.Len(t, tests, 4)
	levelText := func(tc reportTest) string {
		got, err := os.ReadFile(filepath.Join(evals, tc.Vars.ReportInstructions[len("file://"):]))
		require.NoError(t, err)
		return string(got)
	}
	assert.Equal(t, `Say "Reads with growing confidence." now.`, levelText(tests[0]))
	assert.Equal(t, "Snapshot.", levelText(tests[1]), "a Level with no candidate keeps its snapshot")
	assert.Equal(t, `Say "Reads with growing confidence." now.`, levelText(tests[2]))
	assert.Equal(t, "Say  now.", levelText(tests[3]), "strip_level_examples applies to the candidate too")

	for i, want := range []*bool{new(bool), new(bool), nil, nil} {
		assert.Equal(t, want, tests[i].Vars.UseReference, tests[i].Description)
	}
}

func TestLoadTweakCase(t *testing.T) {
	db, refs := seedReferenceStore(t)
	setLevelSnapshot(t, refs, `Say "Reads with growing confidence." or leave a placeholder.`)
	resolver := handler.NewReportInputResolver(db)
	entry := func(tw reportTweak) reportManifestEntry {
		return reportManifestEntry{ID: "t", Description: "d", StudentID: 1, Tweak: &tw}
	}

	c, err := loadTweakCase(context.Background(), resolver, refs, entry(reportTweak{Of: "c", Paragraph: 0, Ask: "longer"}))
	require.NoError(t, err)
	assert.Equal(t, "<p>ref</p>", c.ReferenceHTML, "the base case's reference is the draft")
	assert.Equal(t, []string{"longer"}, c.Feedback)
	assert.Equal(t, "ref", c.ParagraphText)
	assert.Contains(t, c.ReportInstructions, "Reads with growing confidence.")

	c, err = loadTweakCase(context.Background(), resolver, refs, entry(reportTweak{Of: "c", Ask: "longer", StripLevelExamples: true}))
	require.NoError(t, err)
	assert.Equal(t, "Say  or leave a placeholder.", c.ReportInstructions)

	_, err = loadTweakCase(context.Background(), resolver, refs, entry(reportTweak{Of: "c", Paragraph: 1, Ask: "longer"}))
	assert.ErrorContains(t, err, "tweak paragraph 1")
}

func TestLoadReportCases_FreshRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		stmt    string
		student int64
		id      string
		want    string
	}{
		"note added": {stmt: `INSERT INTO notes (id, student_id, date, summary) VALUES (50, 1, '2026-03-20', 'later')`, want: `case "c": notes in range differ from the reference's (extra 50)`},
		// A prod copy can hold notes the local DB never had, created before the reference.
		"note added, older timestamp": {stmt: `INSERT INTO notes (id, student_id, date, summary, created_at, updated_at) VALUES (51, 1, '2026-03-20', 'x', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, want: "(extra 51)"},
		"note deleted":                {stmt: `DELETE FROM notes WHERE id = 1`, want: "(missing 1)"},
		"note edited":                 {stmt: `UPDATE notes SET summary = 'changed', updated_at = '2030-01-01T00:00:00Z' WHERE id = 2`, want: "(edited 2)"},
		"several":                     {stmt: `DELETE FROM notes WHERE id = 1; INSERT INTO notes (id, student_id, date, summary) VALUES (50, 1, '2026-03-20', 'x')`, want: "(missing 1; extra 50)"},
		"wrong student":               {student: 2, want: "reference is for student 1, not student 2"},
		"no reference":                {id: "other", want: `case "other": reference:`},
	} {
		t.Run(name, func(t *testing.T) {
			db, refs := seedReferenceStore(t)
			if tc.stmt != "" {
				_, err := db.Exec(tc.stmt)
				require.NoError(t, err)
			}
			e := reportManifestEntry{ID: "c", Description: "d", StudentID: 1}
			if tc.student != 0 {
				e.StudentID = tc.student
			}
			if tc.id != "" {
				e.ID = tc.id
			}
			path := filepath.Join(t.TempDir(), "db.sqlite")
			_, err := db.Exec(`VACUUM INTO ?`, path)
			require.NoError(t, err)
			_, err = loadReportCases(path, refs, reportManifest{Reports: []reportManifestEntry{e}})
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestWriteReference_RefusesOverwrite(t *testing.T) {
	refs := t.TempDir()
	require.NoError(t, writeReference(refs, "c", referenceMeta{StudentID: 1}, "<p>first</p>"))
	assert.ErrorContains(t, writeReference(refs, "c", referenceMeta{StudentID: 1}, "<p>second</p>"), "exists")
	_, html, err := readReference(refs, "c")
	require.NoError(t, err)
	assert.Equal(t, "<p>first</p>", html)
}
