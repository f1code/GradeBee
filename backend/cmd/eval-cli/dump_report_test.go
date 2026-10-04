package main

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	handler "github.com/nicogaller/gradebee/backend"
)

// seedDumpDB gives student 1 (Élodie, alias Lulu) report 10 in class 1 with
// classmates Ann and Bob, and puts Zoé in another class.
func seedDumpDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := handler.OpenDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, handler.RunMigrations(db))
	for _, stmt := range []string{
		`INSERT INTO levels (id, group_id, name, report_instructions) VALUES (1, 'g', 'Marcia', 'Three sections.')`,
		`INSERT INTO classes (id, user_id, level_id, day) VALUES (1, 'u', 1, 'Monday'), (2, 'u', 1, 'Tuesday')`,
		`INSERT INTO students (id, class_id, name) VALUES (1, 1, 'Élodie'), (2, 1, 'Ann'), (3, 1, 'Bob'), (4, 2, 'Zoé')`,
		`INSERT INTO student_aliases (student_id, class_id, alias) VALUES (1, 1, 'Lulu'), (3, 1, 'Bobby')`,
		`INSERT INTO notes (student_id, date, summary) VALUES
			(1, '2026-03-10', 'ÉLODIE played the Marcia piece with ann; Annual recital next.'),
			(1, '2026-03-11', 'Lulu and Bobby argued; Bob''s sister came. Zoé visited.')`,
		`INSERT INTO reports (id, student_id, start_date, end_date, html, instructions, created_at) VALUES
			(10, 1, '2026-03-01', '2026-03-31', '<p>Élodie helped Ann.</p>', 'Mention Lulu''s solo', '2026-04-03T00:00:00Z')`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	return db
}

func TestDumpReport_RedactsRoster(t *testing.T) {
	db := seedDumpDB(t)
	var out bytes.Buffer
	require.NoError(t, dumpReport(context.Background(), db, &out, 1, 10))
	got := out.String()

	// "Annual" never occurs lowercase in this DB, so the capitalized pass takes it.
	assert.Contains(t, got, "2026-03-10: STUDENT played the Marcia piece with CLASSMATE_1; NAME_1 recital next.")
	assert.Contains(t, got, "2026-03-11: STUDENT and CLASSMATE_2 argued; CLASSMATE_2's sister came. Zoé visited.")
	assert.Contains(t, got, "Mention STUDENT's solo")
	assert.Contains(t, got, "<p>STUDENT helped CLASSMATE_1.</p>")
	lower := strings.ToLower(got)
	for _, name := range []string{"élodie", "lulu", "bob", "bobby"} {
		assert.NotContains(t, lower, name)
	}
}

func TestRedact_WordBoundaries(t *testing.T) {
	r := newRedactor([]handler.Student{
		{ID: 1, Name: "Ann", Aliases: []string{"Ann Lee"}},
		{ID: 2, Name: "Noé"},
	}, 1)
	cases := map[string]string{
		"Annual Anna Ann.":  "Annual Anna STUDENT.",
		"ann lee came":      "STUDENT came",
		"Noé, Noémie, éNoé": "CLASSMATE_1, Noémie, éNoé",
		"Ann-Noé":           "STUDENT-CLASSMATE_1",
		"Ann":               "STUDENT",
	}
	for in, want := range cases {
		assert.Equal(t, want, r.redact(in), in)
	}
}

func TestRedact_FoldsDiacritics(t *testing.T) {
	r := newRedactor([]handler.Student{
		{ID: 1, Name: "Élodie"},
		{ID: 2, Name: "Chloe", Aliases: []string{"Zoë Ann"}},
		{ID: 3, Name: "Ine\u0301s"},
		{ID: 4, Name: "Søren"},
		{ID: 5, Name: "Jean-Luc", Aliases: []string{"Marie Claire"}},
		{ID: 6, Name: "Strauß", Aliases: []string{"Œdipe"}},
	}, 1)
	cases := map[string]string{
		"Elodie and ELODIE":      "STUDENT and STUDENT",
		"Chloé, CHLOË, Zoe Ann.": "CLASSMATE_1, CLASSMATE_1, CLASSMATE_1.",
		// NFD: base letter plus combining acute, trailing mark included.
		"E\u0301lodie met Chloe\u0301!": "STUDENT met CLASSMATE_1!",
		"Elodies Chloéa":                "Elodies Chloéa",
		"Inés, Ines":                    "CLASSMATE_2, CLASSMATE_2",
		"Soren, ＳＯＲＥＮ":                  "CLASSMATE_3, CLASSMATE_3",
		"Jean Luc, Marie  Claire, Marie\u00a0Claire, Marie\nClaire": "CLASSMATE_4, CLASSMATE_4, CLASSMATE_4, CLASSMATE_4",
		"Strauss, Oedipe, STRAUSS":                                  "CLASSMATE_5, CLASSMATE_5, CLASSMATE_5",
		"café Élodie":                                               "café STUDENT",
	}
	for in, want := range cases {
		assert.Equal(t, want, r.redact(in), in)
	}
}

func TestDumpReport_RedactsOffRosterCapitals(t *testing.T) {
	db := seedDumpDB(t)
	// Another Group's Level words do not count as Level names here.
	_, err := db.Exec(`INSERT INTO levels (group_id, name) VALUES ('other', 'Ruiz')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO notes (student_id, date, summary) VALUES
		(1, '2026-03-12', 'Gwen sang. She and Gwen hugged Marcia in May. The class met Mrs Ruiz, and the teacher said I can.'),
		(1, '2026-03-13', 'Teacher: Jane Doe. (Learning) went well; she sang with "Gwen".')`)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, dumpReport(context.Background(), db, &out, 1, 10))
	got := out.String()

	assert.Contains(t, got, "2026-03-12: NAME_2 sang. She and NAME_2 hugged Marcia in May. The class met NAME_3 NAME_4, and the teacher said I can.")
	assert.Contains(t, got, "2026-03-13: Teacher: NAME_5 NAME_6. (Learning) went well; she sang with \"NAME_2\".")
}

func TestRedactCapitalized(t *testing.T) {
	allowed := map[string]bool{"the": true, "Marcia": true}
	cases := map[string]string{
		// Sentence starts, line starts, element text and opening quotes stay.
		"Ivo ran. Ivo sat!\nIvo <p>Ivo</p> (\"Ivo\")": "Ivo ran. Ivo sat!\nIvo <p>Ivo</p> (\"Ivo\")",
		// Once seen mid-sentence, every capitalized occurrence goes; lowercase stays.
		"Ivo ran with Ana. Ana, ana.":         "Ivo ran with NAME_1. NAME_1, ana.",
		"Name: Ana":                           "Name: NAME_1",
		"say The, Marcia, CLASSMATE_1, I, OK": "say The, Marcia, CLASSMATE_1, I, OK",
		"with Éva\u0301 and Zoe\u0301":        "with NAME_1 and NAME_2",
	}
	for in, want := range cases {
		assert.Equal(t, want, redactCapitalized([]string{in}, allowed)[0], in)
	}
	got := redactCapitalized([]string{"Bo sat.", "Ivo met Bo."}, allowed)
	assert.Equal(t, []string{"NAME_1 sat.", "Ivo met NAME_1."}, got, "numbering and replacement span texts")
}

func TestDumpReport_Errors(t *testing.T) {
	db := seedDumpDB(t)
	cases := map[string]struct {
		studentID, reportID int64
		want                string
	}{
		"unknown report":  {studentID: 1, reportID: 99, want: "report 99: not found"},
		"unknown student": {studentID: 9, reportID: 10, want: "belongs to student 1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := dumpReport(context.Background(), db, &out, tc.studentID, tc.reportID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, out.String())
		})
	}
}

func TestRun_DumpReportUsage(t *testing.T) {
	err := run([]string{"eval-cli", "dump-report", "-db", "x.db", "-student", "1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage: eval-cli dump-report")
}
