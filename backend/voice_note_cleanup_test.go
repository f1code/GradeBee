package handler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCleanup_TranscriptLeavesWithRow pins the transcript's lifetime: it is not
// cleared when the audio is purged (see TestProcessJob_DeletesAudioAfterTranscription)
// but goes when the retention cleanup deletes the row. Rows: a done job past
// retention, a job that failed after transcription and was never dismissed (no
// processed_at, only created_at), a fresh unprocessed job, and a job uploaded long
// ago but processed just now. The first two go; the last two stay — an abandoned
// row must not keep its transcript forever, and a late retry must not lose it.
func TestCleanup_TranscriptLeavesWithRow(t *testing.T) {
	db := setupTestDB(t)
	repo := &VoiceNoteRepo{db: db}
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")

	lateRetry, err := repo.Create(ctx, "u1", "late.m4a", "")
	require.NoError(t, err)
	require.NoError(t, repo.SetTranscript(ctx, lateRetry.ID, "late words"))
	require.NoError(t, repo.MarkProcessed(ctx, lateRetry.ID))
	_, err = db.Exec(`UPDATE voice_notes SET created_at = ? WHERE id = ?`, old, lateRetry.ID)
	require.NoError(t, err)

	done, err := repo.Create(ctx, "u1", "done.m4a", "")
	require.NoError(t, err)
	require.NoError(t, repo.SetTranscript(ctx, done.ID, "done words"))
	require.NoError(t, repo.MarkPurged(ctx, done.ID))
	_, err = db.Exec(`UPDATE voice_notes SET processed_at = ? WHERE id = ?`, old, done.ID)
	require.NoError(t, err)

	abandoned, err := repo.Create(ctx, "u1", "failed.m4a", "")
	require.NoError(t, err)
	require.NoError(t, repo.SetTranscript(ctx, abandoned.ID, "failed words"))
	require.NoError(t, repo.MarkPurged(ctx, abandoned.ID))
	_, err = db.Exec(`UPDATE voice_notes SET created_at = ? WHERE id = ?`, old, abandoned.ID)
	require.NoError(t, err)

	fresh, err := repo.Create(ctx, "u1", "new.m4a", "")
	require.NoError(t, err)
	require.NoError(t, repo.SetTranscript(ctx, fresh.ID, "new words"))

	cleanProcessedVoiceNotes(ctx, repo, 24*time.Hour)

	_, err = repo.GetByID(ctx, done.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "done row past retention should be deleted, got %v", err)
	_, err = repo.GetByID(ctx, abandoned.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "abandoned row past retention should be deleted, got %v", err)

	got, err := repo.GetByID(ctx, fresh.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Transcript)
	assert.Equal(t, "new words", *got.Transcript, "fresh unprocessed row keeps its transcript")

	got, err = repo.GetByID(ctx, lateRetry.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Transcript)
	assert.Equal(t, "late words", *got.Transcript, "row processed within retention keeps its transcript, however old the upload")
}

// TestCleanup_RemovesOrphanedAudio: a job that failed at or before transcription
// leaves its audio on disk with purged_at unset. Once the row is old enough the
// cleanup deletes the file and then the row; a file already gone does not block
// the row.
func TestCleanup_RemovesOrphanedAudio(t *testing.T) {
	db := setupTestDB(t)
	repo := &VoiceNoteRepo{db: db}
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")

	audioPath := filepath.Join(t.TempDir(), "orphan.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))
	withFile, err := repo.Create(ctx, "u1", "orphan.m4a", audioPath)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE voice_notes SET created_at = ? WHERE id = ?`, old, withFile.ID)
	require.NoError(t, err)

	gonePath := filepath.Join(t.TempDir(), "gone.m4a")
	fileGone, err := repo.Create(ctx, "u1", "gone.m4a", gonePath)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE voice_notes SET created_at = ? WHERE id = ?`, old, fileGone.ID)
	require.NoError(t, err)

	cleanProcessedVoiceNotes(ctx, repo, 24*time.Hour)

	_, statErr := os.Stat(audioPath)
	assert.True(t, os.IsNotExist(statErr), "orphaned audio should be deleted")
	_, err = repo.GetByID(ctx, withFile.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "row with orphaned audio should be deleted, got %v", err)
	_, err = repo.GetByID(ctx, fileGone.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "row whose file is already gone should be deleted, got %v", err)
}

// TestVoiceNoteRepo_MarkProcessedDoesNotRewind: dismissing a done job calls
// MarkProcessed again; the retention window must keep counting from the first
// call, or a dismiss on day 6 would keep the transcript to day 13.
func TestVoiceNoteRepo_MarkProcessedDoesNotRewind(t *testing.T) {
	db := setupTestDB(t)
	repo := &VoiceNoteRepo{db: db}
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")

	vn, err := repo.Create(ctx, "u1", "a.m4a", "")
	require.NoError(t, err)
	require.NoError(t, repo.MarkProcessed(ctx, vn.ID))
	_, err = db.Exec(`UPDATE voice_notes SET processed_at = ? WHERE id = ?`, old, vn.ID)
	require.NoError(t, err)

	require.NoError(t, repo.MarkProcessed(ctx, vn.ID))
	got, err := repo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProcessedAt)
	assert.Equal(t, old, *got.ProcessedAt)
}

func TestVoiceNoteRepo_SetTranscriptNotFound(t *testing.T) {
	repo := &VoiceNoteRepo{db: setupTestDB(t)}
	err := repo.SetTranscript(context.Background(), 999, "x")
	assert.True(t, errors.Is(err, ErrNotFound), "got %v", err)
}
