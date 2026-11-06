package handler

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrateBefore applies every migration sorting before prefix.
func migrateBefore(t *testing.T, prefix string) *sql.DB {
	t.Helper()
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE _migrations (name TEXT PRIMARY KEY, applied_at TEXT)`)
	require.NoError(t, err)
	entries, err := migrations.ReadDir("sql")
	require.NoError(t, err)
	for _, e := range entries {
		if e.Name() >= prefix {
			continue
		}
		content, err := migrations.ReadFile("sql/" + e.Name())
		require.NoError(t, err)
		_, err = db.Exec(string(content))
		require.NoError(t, err, e.Name())
		_, err = db.Exec("INSERT INTO _migrations (name) VALUES (?)", e.Name())
		require.NoError(t, err)
	}
	return db
}

// 021: each Group gets Term 1 holding its Levels' text and every Report.
func TestMigration021_Terms(t *testing.T) {
	db := migrateBefore(t, "021_")
	for _, q := range []string{
		`INSERT INTO levels (id, group_id, name, report_instructions) VALUES
			(1, 'g', 'Mousy', 'Three sections.'), (2, 'g', 'Sam', '  '), (3, 'h', 'Emma', 'Brief.')`,
		`INSERT INTO classes (id, user_id, level_id, day) VALUES (1, 'u1', 1, 'Monday'), (2, 'u2', 3, 'Monday')`,
		`INSERT INTO students (id, class_id, name) VALUES (1, 1, 'Alice'), (2, 2, 'Bob')`,
		`INSERT INTO reports (id, student_id, start_date, end_date, html, level_instructions)
			VALUES (1, 1, '2026-09-01', '2026-10-01', '<p>a</p>', 'Three sections.'), (2, 2, '2026-09-01', '2026-10-01', '<p>b</p>', NULL)`,
	} {
		_, err := db.Exec(q)
		require.NoError(t, err)
	}

	require.NoError(t, RunMigrations(db))

	terms := map[string]int64{}
	rows, err := db.Query(`SELECT id, group_id, name, start_date, end_date, archived_at FROM terms`)
	require.NoError(t, err)
	for rows.Next() {
		var tm Term
		require.NoError(t, rows.Scan(&tm.ID, &tm.GroupID, &tm.Name, &tm.StartDate, &tm.EndDate, &tm.ArchivedAt))
		assert.Equal(t, Term{ID: tm.ID, GroupID: tm.GroupID, Name: "Term 1", StartDate: "2026-08-15", EndDate: "2026-11-01"}, tm)
		terms[tm.GroupID] = tm.ID
	}
	require.NoError(t, rows.Err())
	require.Len(t, terms, 2)

	termRepo := &TermRepo{db: db}
	for _, tc := range []struct {
		level int64
		group string
		want  string
	}{{1, "g", "Three sections."}, {2, "g", ""}, {3, "h", "Brief."}} {
		got, err := termRepo.Instructions(t.Context(), tc.level, terms[tc.group])
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "level %d", tc.level)
	}

	reports := &ReportRepo{db: db}
	for id, group := range map[int64]string{1: "g", 2: "h"} {
		rpt, err := reports.GetByID(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, terms[group], rpt.TermID, "report %d", id)
	}
	a, err := reports.GetByID(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, "Three sections.", *a.LevelInstructions)

	_, err = db.Exec(`SELECT report_instructions FROM levels`)
	assert.Error(t, err, "levels.report_instructions dropped")
	_, err = db.Exec(`DELETE FROM terms WHERE id = ?`, terms["g"])
	assert.Error(t, err, "a Term with Reports cannot be deleted")
}
