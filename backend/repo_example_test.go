package handler

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportExampleRepo_SetAndGetClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ReportExampleRepo{db: db}

	// Create an example first.
	e, err := repo.Create(t.Context(), "user1", "Test Example", "some content")
	require.NoError(t, err, "create")

	// Set class names.
	classNames := []string{"Grade 4", "Grade 5"}
	require.NoError(t, repo.SetClassNames(t.Context(), e.ID, classNames), "SetClassNames")

	// Get them back.
	got, err := repo.GetClassNames(t.Context(), e.ID)
	require.NoError(t, err, "GetClassNames")
	assert.True(t, slices.Equal(got, classNames), "GetClassNames = %v, want %v", got, classNames)

	// Replace with new set.
	require.NoError(t, repo.SetClassNames(t.Context(), e.ID, []string{"Grade 6"}), "SetClassNames replace")
	got2, err := repo.GetClassNames(t.Context(), e.ID)
	require.NoError(t, err, "GetClassNames after replace")
	require.Len(t, got2, 1)
	assert.Equal(t, "Grade 6", got2[0])
}

func TestReportExampleRepo_ListReadyByClassName(t *testing.T) {
	db := setupTestDB(t)
	repo := &ReportExampleRepo{db: db}

	// Create examples.
	e1, err := repo.Create(t.Context(), "user1", "Example 1", "content 1")
	require.NoError(t, err, "create e1")
	e2, err := repo.Create(t.Context(), "user1", "Example 2", "content 2")
	require.NoError(t, err, "create e2")
	e3, err := repo.Create(t.Context(), "user1", "Example 3", "content 3")
	require.NoError(t, err, "create e3")

	require.NoError(t, repo.SetClassNames(t.Context(), e1.ID, []string{"Grade 4"}), "SetClassNames e1")
	require.NoError(t, repo.SetClassNames(t.Context(), e2.ID, []string{"Grade 4", "Grade 5"}), "SetClassNames e2")
	require.NoError(t, repo.SetClassNames(t.Context(), e3.ID, []string{"Grade 5"}), "SetClassNames e3")

	// Filter by Grade 4 — should return e1 and e2.
	results, err := repo.ListReadyByClassName(t.Context(), "user1", "Grade 4")
	require.NoError(t, err, "ListReadyByClassName")
	assert.Len(t, results, 2, "Grade 4: unexpected results count")

	// Filter by Grade 5 — should return e2 and e3.
	results, err = repo.ListReadyByClassName(t.Context(), "user1", "Grade 5")
	require.NoError(t, err, "ListReadyByClassName")
	assert.Len(t, results, 2, "Grade 5: unexpected results count")

	// Empty className — should return all 3.
	results, err = repo.ListReadyByClassName(t.Context(), "user1", "")
	require.NoError(t, err, "ListReadyByClassName empty")
	assert.Len(t, results, 3, "empty className: unexpected results count")

	// Different user — should return nothing.
	results, err = repo.ListReadyByClassName(t.Context(), "user2", "Grade 4")
	require.NoError(t, err, "ListReadyByClassName user2")
	assert.Empty(t, results, "user2: should return nothing")
}
