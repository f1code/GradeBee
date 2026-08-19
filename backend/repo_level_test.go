package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevelRepo_CreateAndList_ScopedByGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)
	assert.Equal(t, "Marcia", l.Name)
	assert.Equal(t, "org_a", l.GroupID)
	assert.Empty(t, l.ReportInstructions)

	_, err = repo.Create(t.Context(), "org_b", "Oliver")
	require.NoError(t, err)

	listA, err := repo.List(t.Context(), "org_a")
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, "Marcia", listA[0].Name)

	listB, err := repo.List(t.Context(), "org_b")
	require.NoError(t, err)
	require.Len(t, listB, 1)
	assert.Equal(t, "Oliver", listB[0].Name)
}

func TestLevelRepo_SameNameDifferentGroups_Allowed(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	_, err := repo.Create(t.Context(), "org_a", "Emma")
	require.NoError(t, err)
	_, err = repo.Create(t.Context(), "org_b", "Emma")
	require.NoError(t, err, "same Level name in different Groups must not collide")
}

func TestLevelRepo_DuplicateNameWithinGroup_ReturnsErrDuplicate(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	_, err := repo.Create(t.Context(), "org_a", "Sam")
	require.NoError(t, err)
	_, err = repo.Create(t.Context(), "org_a", "Sam")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate, got %v", err)
}

func TestLevelRepo_DuplicateNameCaseInsensitive_ReturnsErrDuplicate(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	_, err := repo.Create(t.Context(), "org_a", "Sam")
	require.NoError(t, err)
	_, err = repo.Create(t.Context(), "org_a", "sam")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate for case-only variant, got %v", err)
}

func TestLevelRepo_GetByID_CrossGroup_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Linda")
	require.NoError(t, err)

	// Reachable from the owning Group.
	got, err := repo.GetByID(t.Context(), "org_a", l.ID)
	require.NoError(t, err)
	assert.Equal(t, "Linda", got.Name)

	// Invisible and unreachable from a different Group.
	_, err = repo.GetByID(t.Context(), "org_b", l.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestLevelRepo_Rename(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Mousy")
	require.NoError(t, err)

	require.NoError(t, repo.Rename(t.Context(), "org_a", l.ID, "Mousy Renamed"))

	got, err := repo.GetByID(t.Context(), "org_a", l.ID)
	require.NoError(t, err)
	assert.Equal(t, "Mousy Renamed", got.Name)
}

func TestLevelRepo_Rename_CollisionWithinGroup_ReturnsErrDuplicate(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	_, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)
	target, err := repo.Create(t.Context(), "org_a", "Oliver")
	require.NoError(t, err)

	err = repo.Rename(t.Context(), "org_a", target.ID, "Marcia")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate, got %v", err)
}

func TestLevelRepo_Rename_CollisionWithinGroupCaseInsensitive_ReturnsErrDuplicate(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	_, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)
	target, err := repo.Create(t.Context(), "org_a", "Oliver")
	require.NoError(t, err)

	err = repo.Rename(t.Context(), "org_a", target.ID, "marcia")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate for case-only variant, got %v", err)
}

func TestLevelRepo_Rename_CrossGroup_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)

	err = repo.Rename(t.Context(), "org_b", l.ID, "New Name")
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestLevelRepo_Rename_SameNameAsAnotherGroupsLevel_Allowed(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	_, err := repo.Create(t.Context(), "org_a", "Emma")
	require.NoError(t, err)
	target, err := repo.Create(t.Context(), "org_b", "Oliver")
	require.NoError(t, err)

	// Renaming org_b's Level to "Emma" must not collide with org_a's Level.
	require.NoError(t, repo.Rename(t.Context(), "org_b", target.ID, "Emma"))
}

func TestLevelRepo_UpdateReportInstructions(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)

	require.NoError(t, repo.UpdateReportInstructions(t.Context(), "org_a", l.ID, "Focus on reading fluency."))

	got, err := repo.GetByID(t.Context(), "org_a", l.ID)
	require.NoError(t, err)
	assert.Equal(t, "Focus on reading fluency.", got.ReportInstructions)
}

func TestLevelRepo_UpdateReportInstructions_CrossGroup_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)

	err = repo.UpdateReportInstructions(t.Context(), "org_b", l.ID, "nope")
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestLevelRepo_Delete(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)

	require.NoError(t, repo.Delete(t.Context(), "org_a", l.ID))

	_, err = repo.GetByID(t.Context(), "org_a", l.ID)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestLevelRepo_Delete_CrossGroup_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := &LevelRepo{db: db}

	l, err := repo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)

	err = repo.Delete(t.Context(), "org_b", l.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)

	// Confirm it wasn't actually deleted.
	_, err = repo.GetByID(t.Context(), "org_a", l.ID)
	assert.NoError(t, err)
}

func TestLevelRepo_Delete_ReferencedByClass_ReturnsErrLevelInUseAndPreservesData(t *testing.T) {
	db := setupTestDB(t)
	levelRepo := &LevelRepo{db: db}
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}

	l, err := levelRepo.Create(t.Context(), "org_a", "Marcia")
	require.NoError(t, err)

	c1, err := classRepo.Create(t.Context(), "org_a", "user_1", l.ID, "Monday", "")
	require.NoError(t, err)
	c2, err := classRepo.Create(t.Context(), "org_a", "user_1", l.ID, "Monday", "AM")
	require.NoError(t, err)
	s, err := studentRepo.Create(t.Context(), c1.ID, "Alice")
	require.NoError(t, err)

	err = levelRepo.Delete(t.Context(), "org_a", l.ID)
	var inUse *ErrLevelInUse
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, 2, inUse.Count)

	// Level, both Classes, and the Student all survive the refused delete.
	_, err = levelRepo.GetByID(t.Context(), "org_a", l.ID)
	assert.NoError(t, err)
	_, err = classRepo.GetByID(t.Context(), c1.ID)
	assert.NoError(t, err)
	_, err = classRepo.GetByID(t.Context(), c2.ID)
	assert.NoError(t, err)
	_, err = studentRepo.GetByID(t.Context(), s.ID)
	assert.NoError(t, err)
}
