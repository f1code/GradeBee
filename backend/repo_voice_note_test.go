package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
