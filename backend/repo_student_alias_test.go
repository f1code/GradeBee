package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStudentAliasRepo_AddRemoveList covers basic alias CRUD.
func TestStudentAliasRepo_AddRemoveList(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)

	// Add an alias
	a, err := r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err, "add alias")
	assert.Equal(t, "Alex", a.Alias)
	assert.Equal(t, s.ID, a.StudentID)
	assert.NotZero(t, a.ID)

	// List
	aliases, err := r.students.ListAliases(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "Alex", aliases[0].Alias)

	// Remove
	require.NoError(t, r.students.RemoveAlias(ctx, s.ID, a.ID))
	aliases, err = r.students.ListAliases(ctx, s.ID)
	require.NoError(t, err)
	assert.Empty(t, aliases)
}

// TestStudentAliasRepo_DuplicateAlias checks that duplicate aliases are rejected
// and the error carries the owner's name.
func TestStudentAliasRepo_DuplicateAlias(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)

	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Same alias again → *ErrDuplicateAlias with the owner's name
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	var dupErr *ErrDuplicateAlias
	require.ErrorAs(t, err, &dupErr, "expected *ErrDuplicateAlias, got: %v", err)
	assert.Equal(t, "Alexander", dupErr.ConflictStudentName)
}

// TestStudentAliasRepo_DuplicateCaseInsensitive checks that duplicate check is case-insensitive.
func TestStudentAliasRepo_DuplicateCaseInsensitive(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)

	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Same alias, different case → *ErrDuplicateAlias
	_, err = r.students.AddAlias(ctx, s.ID, "ALEX")
	var dupErr *ErrDuplicateAlias
	require.ErrorAs(t, err, &dupErr, "expected *ErrDuplicateAlias for case variant, got: %v", err)
	assert.Equal(t, "Alexander", dupErr.ConflictStudentName)
}

// TestStudentAliasRepo_AliasCollidesWithName checks that an alias can't match another student's canonical name,
// and that the error includes that student's name.
func TestStudentAliasRepo_AliasCollidesWithName(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s1, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.Create(ctx, c.ID, "Alex")
	require.NoError(t, err)

	// Adding "Alex" as alias for Alexander should fail — Alex is a student name in the same class
	_, err = r.students.AddAlias(ctx, s1.ID, "Alex")
	var dupErr *ErrDuplicateAlias
	require.ErrorAs(t, err, &dupErr, "alias should collide with existing student name, got: %v", err)
	assert.Equal(t, "Alex", dupErr.ConflictStudentName)
}

// TestFindByNameAndClass_MatchesAlias verifies that FindByNameAndClass resolves
// an alias to the canonical student.
func TestFindByNameAndClass_MatchesAlias(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Lookup by alias — should return the canonical student ID
	id, err := r.students.FindByNameAndClass(ctx, "Alex", "Math", "user1")
	require.NoError(t, err, "find by alias")
	assert.Equal(t, s.ID, id)
}

// TestFindByNameAndClass_MatchesCaseInsensitive checks case-insensitive matching.
func TestFindByNameAndClass_MatchesCaseInsensitive(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Case-insensitive alias match
	id, err := r.students.FindByNameAndClass(ctx, "alex", "Math", "user1")
	require.NoError(t, err, "find by lowercase alias")
	assert.Equal(t, s.ID, id)

	// Case-insensitive canonical name match
	id, err = r.students.FindByNameAndClass(ctx, "alexander", "Math", "user1")
	require.NoError(t, err, "find by lowercase canonical")
	assert.Equal(t, s.ID, id)
}

// TestListWithAliases verifies alias strings are populated in ListWithAliases.
func TestListWithAliases(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Xander")
	require.NoError(t, err)

	students, err := r.students.ListWithAliases(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, students, 1)
	assert.ElementsMatch(t, []string{"Alex", "Xander"}, students[0].Aliases)
}

// TestAliasDeleteCascadesWithStudent verifies aliases are deleted when student is deleted.
func TestAliasDeleteCascadesWithStudent(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	a, err := r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	require.NoError(t, r.students.Delete(ctx, s.ID))

	// The alias should be gone — RemoveAlias with original student ID should return ErrNotFound
	err = r.students.RemoveAlias(ctx, s.ID, a.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "alias should be cascade-deleted, got: %v", err)
}

// TestBuildExtractionPrompt_AliasesIncluded verifies that aliases appear in the prompt.
func TestBuildExtractionPrompt_AliasesIncluded(t *testing.T) {
	classes := []ClassGroup{
		{
			Name: "Period 1",
			Students: []ClassStudent{
				{Name: "Alexander", Aliases: []string{"Alex", "Xander"}},
				{Name: "Katherine"},
			},
		},
	}
	prompt := BuildExtractionPrompt(classes)

	assert.True(t, strings.Contains(prompt, "Alexander (aka Alex, Xander)"),
		"prompt missing alias line, got: %s", prompt)
	assert.True(t, strings.Contains(prompt, "Katherine (class_name Period 1)"),
		"prompt missing no-alias line, got: %s", prompt)
	assert.True(t, strings.Contains(prompt, "return the canonical name"),
		"prompt missing alias instruction, got: %s", prompt)
}

// TestRemoveAlias_WrongStudent verifies alias ID is scoped to the student.
func TestRemoveAlias_WrongStudent(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s1, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	s2, err := r.students.Create(ctx, c.ID, "Katherine")
	require.NoError(t, err)

	a, err := r.students.AddAlias(ctx, s1.ID, "Alex")
	require.NoError(t, err)

	// Try to remove Alex's alias but pass s2's ID — should fail
	err = r.students.RemoveAlias(ctx, s2.ID, a.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "should not delete alias belonging to another student, got: %v", err)

	// Original alias still exists
	aliases, err := r.students.ListAliases(ctx, s1.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
}

// TestCreateStudent_CollidesWithAlias checks that a new student name can't match an existing alias.
func TestCreateStudent_CollidesWithAlias(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Creating a new student named "Alex" should fail — Alex is already an alias
	_, err = r.students.Create(ctx, c.ID, "Alex")
	assert.True(t, errors.Is(err, ErrDuplicate), "new student name should collide with existing alias, got: %v", err)
}

// TestRenameStudent_CollidesWithAlias checks that renaming a student can't produce a name matching another student's alias.
func TestRenameStudent_CollidesWithAlias(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s1, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	s2, err := r.students.Create(ctx, c.ID, "Bob")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s1.ID, "Alex")
	require.NoError(t, err)

	// Renaming Bob to "Alex" should fail — Alex is already an alias for Alexander
	err = r.students.Rename(ctx, s2.ID, "Alex")
	assert.True(t, errors.Is(err, ErrDuplicate), "rename should collide with existing alias, got: %v", err)
}

func TestFindByNameAndClass_AliasNotFoundInDifferentClass(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))
	t.Cleanup(func() { db.Close() })

	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}

	c1, err := classRepo.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	_, err = classRepo.Create(ctx, "user1", "Science", "")
	require.NoError(t, err)

	s1, err := studentRepo.Create(ctx, c1.ID, "Alexander")
	require.NoError(t, err)
	_, err = studentRepo.AddAlias(ctx, s1.ID, "Alex")
	require.NoError(t, err)

	// Alex alias exists in Math, not in Science
	_, err = studentRepo.FindByNameAndClass(ctx, "Alex", "Science", "user1")
	assert.True(t, errors.Is(err, ErrNotFound), "alias should not match across classes, got: %v", err)

	// Should still work for Math
	id, err := studentRepo.FindByNameAndClass(ctx, "Alex", "Math", "user1")
	require.NoError(t, err)
	assert.Equal(t, s1.ID, id)
}
