package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassRepo_CreateWithClassNameGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	c, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	require.NoError(t, err)
	assert.Equal(t, "Mousy", c.ClassName)
	assert.Equal(t, "Thursday", c.GroupName)
	assert.Equal(t, "Mousy-Thursday", c.Name)
}

func TestClassRepo_CreateNoGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	c, err := repo.Create(t.Context(), "user1", "Lions", "")
	require.NoError(t, err)
	assert.Equal(t, "Lions", c.Name)
	assert.Empty(t, c.GroupName)
}

func TestClassRepo_ListDistinctClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	for _, args := range [][2]string{
		{"Bears", "Monday"},
		{"Bears", "Tuesday"},
		{"Lions", ""},
		{"Tigers", "AM"},
	} {
		_, err := repo.Create(t.Context(), "user1", args[0], args[1])
		require.NoError(t, err)
	}

	names, err := repo.ListDistinctClassNames(t.Context(), "user1")
	require.NoError(t, err)
	want := []string{"Bears", "Lions", "Tigers"}
	require.Len(t, names, len(want), "got %v, want %v", names, want)
	for i, n := range names {
		assert.Equal(t, want[i], n)
	}
}

func TestClassRepo_DuplicateClassGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	_, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	require.NoError(t, err)
	_, err = repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate, got %v", err)
}
