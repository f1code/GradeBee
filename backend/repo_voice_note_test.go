package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every recording gets its own key at creation, and every read hands it back:
// it is what a note carries to name its recording after the row is gone.
func TestVoiceNoteRepo_CreateMintsATraceID(t *testing.T) {
	db := setupTestDB(t)
	repo := &VoiceNoteRepo{db: db}
	ctx := context.Background()

	first, err := repo.Create(ctx, "user1", "one.mp3", "/tmp/one.mp3")
	require.NoError(t, err)
	second, err := repo.Create(ctx, "user1", "two.mp3", "/tmp/two.mp3")
	require.NoError(t, err)

	_, err = uuid.Parse(first.TraceID)
	require.NoError(t, err, "a UUID, not %q", first.TraceID)
	assert.NotEqual(t, first.TraceID, second.TraceID)

	got, err := repo.GetByID(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, first.TraceID, got.TraceID)

	stale, err := repo.ListStale(ctx, "9999-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, stale, 2)
	assert.Equal(t, first.TraceID, stale[0].TraceID)
}

func TestVoiceNoteRepo_MarkPurged(t *testing.T) {
	db := setupTestDB(t)
	repo := &VoiceNoteRepo{db: db}
	ctx := context.Background()

	vn, err := repo.Create(ctx, "user1", "rec.mp3", "/tmp/rec.mp3")
	require.NoError(t, err)

	// Mark processed first
	require.NoError(t, repo.MarkProcessed(ctx, vn.ID))

	// PurgedAt should be nil before marking purged
	got, err := repo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	assert.Nil(t, got.PurgedAt, "PurgedAt should be nil before MarkPurged")

	// Mark purged
	require.NoError(t, repo.MarkPurged(ctx, vn.ID))

	got, err = repo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.PurgedAt, "PurgedAt should be set after MarkPurged")
}
