package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Recordings that predate 017 get a key so the unique index holds; notes that
// predate it get none and stay readable. Nothing can say which recording an
// old note came from, and a made-up key would be a lie the guard could act on.
func TestMigration017_BackfillsRecordingsNotNotes(t *testing.T) {
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
	// Everything before 017, in order, and nothing after it: a later
	// migration must not run ahead of the one under test.
	for _, e := range entries {
		if e.Name() >= "017_" {
			continue
		}
		content, err := migrations.ReadFile("sql/" + e.Name())
		require.NoError(t, err)
		_, err = db.Exec(string(content))
		require.NoError(t, err, e.Name())
		_, err = db.Exec("INSERT INTO _migrations (name) VALUES (?)", e.Name())
		require.NoError(t, err)
	}

	// Two recordings and a note from before the column existed.
	_, err = db.Exec(`INSERT INTO voice_notes (user_id, file_name, file_path) VALUES ('u1', 'a.m4a', '/a'), ('u1', 'b.m4a', '/b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO levels (group_id, name) VALUES ('g', 'L')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO classes (user_id, level_id, day) VALUES ('u1', 1, 'Monday')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO students (class_id, name) VALUES (1, 'Alice')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO notes (student_id, date, summary) VALUES (1, '2026-09-01', 'old')`)
	require.NoError(t, err)

	require.NoError(t, RunMigrations(db))

	ctx := context.Background()
	voiceNotes := &VoiceNoteRepo{db: db}
	a, err := voiceNotes.GetByID(ctx, 1)
	require.NoError(t, err)
	b, err := voiceNotes.GetByID(ctx, 2)
	require.NoError(t, err)
	assert.NotEmpty(t, a.TraceID)
	assert.NotEmpty(t, b.TraceID)
	assert.NotEqual(t, a.TraceID, b.TraceID, "a random id per row, not one shared value")

	notes := &NoteRepo{db: db}
	old, err := notes.GetByID(ctx, 1)
	require.NoError(t, err)
	assert.Nil(t, old.TraceID)
	listed, err := notes.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].TraceID)
}
