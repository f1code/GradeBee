package handler

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 019 rebuilds reports; the rows must survive column for column, and the new
// AUTOINCREMENT counter must start above every id a feedback row still names,
// whether that id is a live report or a deleted one.
func TestMigration019_KeepsReportsAndSeedsAboveFeedbackIDs(t *testing.T) {
	tests := []struct {
		name string
		// extraReport, when non-zero, is a second surviving report's id.
		extraReport int64
		// deleteExtra drops that report before migrating, leaving only feedback to name it.
		deleteExtra bool
		feedbackID  int64
		wantNext    int64
	}{
		{name: "feedback names the highest id", extraReport: 2, deleteExtra: true, feedbackID: 5, wantNext: 6},
		{name: "live report holds the highest id", extraReport: 9, feedbackID: 5, wantNext: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := OpenDB(":memory:")
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })

			_, err = db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
				name       TEXT PRIMARY KEY,
				applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			)`)
			require.NoError(t, err)
			entries, err := migrations.ReadDir("sql")
			require.NoError(t, err)
			// Nothing at or after 019 may run ahead of the one under test.
			for _, e := range entries {
				if e.Name() >= "019_" {
					continue
				}
				content, err := migrations.ReadFile("sql/" + e.Name())
				require.NoError(t, err)
				_, err = db.Exec(string(content))
				require.NoError(t, err, e.Name())
				_, err = db.Exec("INSERT INTO _migrations (name) VALUES (?)", e.Name())
				require.NoError(t, err)
			}

			_, err = db.Exec(`INSERT INTO levels (group_id, name) VALUES ('g', 'L')`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO classes (user_id, level_id, day) VALUES ('u1', 1, 'Monday')`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO students (class_id, name) VALUES (1, 'Alice')`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO reports (id, student_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash)
				VALUES (1, 1, '2026-01-01', '2026-03-31', '<p>one</p>', 'be kind', '2026-04-01T00:00:00.000Z', 'model-a', 'hash-a')`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO reports (id, student_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash)
				VALUES (?, 1, '2026-02-01', '2026-04-30', '<p>two</p>', 'be brief', '2026-05-01T00:00:00.000Z', 'model-b', 'hash-b')`, tt.extraReport)
			require.NoError(t, err)
			if tt.deleteExtra {
				_, err = db.Exec(`DELETE FROM reports WHERE id = ?`, tt.extraReport)
				require.NoError(t, err)
			}
			_, err = db.Exec(`INSERT INTO artifact_feedback (artifact_type, artifact_id, rating, signal, comment, user_id)
				VALUES ('report', ?, 'down', 'regenerated', 'shorter', 'u1')`, tt.feedbackID)
			require.NoError(t, err)

			require.NoError(t, RunMigrations(db))

			ctx := context.Background()
			repo := &ReportRepo{db: db}
			got, err := repo.GetByID(ctx, 1)
			require.NoError(t, err)
			str := func(s string) *string { return &s }
			assert.Equal(t, Report{
				ID:           1,
				StudentID:    1,
				StartDate:    "2026-01-01",
				EndDate:      "2026-03-31",
				HTML:         "<p>one</p>",
				Instructions: str("be kind"),
				ModelVersion: str("model-a"),
				PromptHash:   str("hash-a"),
				CreatedAt:    "2026-04-01T00:00:00.000Z",
			}, got)

			assertNextReportID(t, db, tt.wantNext)
		})
	}
}

func assertNextReportID(t *testing.T, db *sql.DB, want int64) {
	t.Helper()
	next := &Report{StudentID: 1, StartDate: "2026-06-01", EndDate: "2026-06-30", HTML: "<p>new</p>"}
	require.NoError(t, (&ReportRepo{db: db}).Create(context.Background(), next))
	assert.Equal(t, want, next.ID)
}
