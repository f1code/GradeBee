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
	require.NoError(t, dumpReport(context.Background(), db, &out, nil, 1, 10))
	got := out.String()

	assert.Contains(t, got, "2026-03-10: STUDENT played the Marcia piece with CLASSMATE_1; Annual recital next.")
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

func TestDumpReport_RedactsMisspelledRoster(t *testing.T) {
	db := seedDumpDB(t)
	_, err := db.Exec(`INSERT INTO notes (student_id, date, summary) VALUES
		(1, '2026-03-12', 'Recently Elodi sang. And Bobb came; recently Marie left.')`)
	require.NoError(t, err)
	var out, repl bytes.Buffer
	require.NoError(t, dumpReport(context.Background(), db, &out, &repl, 1, 10))

	assert.Contains(t, out.String(), "2026-03-12: Recently STUDENT sang. And CLASSMATE_2 came; recently Marie left.")
	assert.Equal(t, "# report 10, student 1\nBobb\tCLASSMATE_2\nElodi\tSTUDENT\n", repl.String())
}

func TestFuzzyRedact(t *testing.T) {
	r := newRedactor([]handler.Student{
		{ID: 1, Name: "John"},
		{ID: 2, Name: "Ann"},
		{ID: 3, Name: "Theo"},
		{ID: 4, Name: "Emma Torres"},
		{ID: 5, Name: "Mara"},
		{ID: 6, Name: "Maya"},
		{ID: 7, Name: "Stuart", Aliases: []string{"Sébastien"}},
	}, 1)
	cases := map[string]string{
		"Recently Jon ran; he left Recently.": "Recently STUDENT ran; he left Recently.",
		"And Marie met him.":                  "And Marie met him.",
		"Then Thea left, then sat.":           "Then CLASSMATE_2 left, then sat.",
		"Ema and Tores sang.":                 "CLASSMATE_3 and CLASSMATE_3 sang.",
		"Mala sat.":                           "CLASSMATE_? sat.",
		"Sebastian and STUDENT, CLASSMATE_1":  "CLASSMATE_6 and STUDENT, CLASSMATE_1",
		"Jón, Jon":                            "STUDENT, STUDENT",
	}
	for in, want := range cases {
		got, _ := r.fuzzyRedact([]string{in})
		assert.Equal(t, want, got[0], in)
	}

	got, tokens := r.fuzzyRedact([]string{"Then Jon sat.", "then he left with Jon."})
	assert.Equal(t, []string{"Then STUDENT sat.", "then he left with STUDENT."}, got, "lowercase and replacements span texts")
	assert.Equal(t, map[string]string{"Jon": "STUDENT"}, tokens)
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
			err := dumpReport(context.Background(), db, &out, nil, tc.studentID, tc.reportID)
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
