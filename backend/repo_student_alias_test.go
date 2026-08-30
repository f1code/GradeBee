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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Lookup by alias — should return the canonical student ID
	id, err := r.students.FindByNameAndClass(ctx, "Alex", "Math · Mon", "user1")
	require.NoError(t, err, "find by alias")
	assert.Equal(t, s.ID, id)
}

// TestFindByNameAndClass_MatchesWithTimeSlot checks that lookup resolves for
// a class name that includes the day and time slot qualifiers
// (e.g. "Math · Thu · Thursday").
func TestFindByNameAndClass_MatchesWithTimeSlot(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "test-group", "user1", testLevelID(t, r.classes.db, "test-group", "Math"), "Thursday", "AM")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)

	id, err := r.students.FindByNameAndClass(ctx, "Alexander", "Math · Thu · AM", "user1")
	require.NoError(t, err, "find by name in qualified class")
	assert.Equal(t, s.ID, id)
}

// TestFindByNameAndClass_MatchesCaseInsensitive checks case-insensitive matching.
func TestFindByNameAndClass_MatchesCaseInsensitive(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	// Case-insensitive alias match
	id, err := r.students.FindByNameAndClass(ctx, "alex", "Math · Mon", "user1")
	require.NoError(t, err, "find by lowercase alias")
	assert.Equal(t, s.ID, id)

	// Case-insensitive canonical name match
	id, err = r.students.FindByNameAndClass(ctx, "alexander", "Math · Mon", "user1")
	require.NoError(t, err, "find by lowercase canonical")
	assert.Equal(t, s.ID, id)
}

// TestListWithAliases verifies ListWithAliases folds the single LEFT JOIN
// query into one entry per student: a student with two aliases (ordered by
// alias, not insertion), a student with none (non-nil empty Aliases), students
// ordered by name regardless of creation order, and nothing from another class
// — even one whose student shares an alias with ours.
func TestListWithAliases(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	// Created out of name order so ORDER BY, not insertion, decides.
	beatrice, err := r.students.Create(ctx, c.ID, "Beatrice")
	require.NoError(t, err)
	alexander, err := r.students.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	// Aliases added out of alphabetical order for the same reason.
	_, err = r.students.AddAlias(ctx, alexander.ID, "Xander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, alexander.ID, "Alex")
	require.NoError(t, err)

	// Another class of the same user with a student + alias that must not leak.
	other := newTestClass(t, r.classes, "test-group", "user1", "Science", "")
	dora, err := r.students.Create(ctx, other.ID, "Dora")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, dora.ID, "Alex")
	require.NoError(t, err)

	students, err := r.students.ListWithAliases(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, students, 2)

	assert.Equal(t, alexander.ID, students[0].ID)
	assert.Equal(t, "Alexander", students[0].Name)
	assert.Equal(t, c.ID, students[0].ClassID)
	assert.Equal(t, []string{"Alex", "Xander"}, students[0].Aliases)

	assert.Equal(t, beatrice.ID, students[1].ID)
	assert.Equal(t, "Beatrice", students[1].Name)
	assert.Equal(t, []string{}, students[1].Aliases, "no aliases must be a non-nil empty slice")

	// Cross-class isolation, paired with the presence arm above (Alexander's
	// "Alex") so the absence can fail.
	for _, s := range students {
		assert.NotEqual(t, "Dora", s.Name)
		assert.NotEqual(t, dora.ID, s.ID)
	}
	otherStudents, err := r.students.ListWithAliases(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, otherStudents, 1)
	assert.Equal(t, "Dora", otherStudents[0].Name)
	assert.Equal(t, []string{"Alex"}, otherStudents[0].Aliases)

	// Empty class: nil, not an empty slice (handler substitutes []Student{}).
	empty := newTestClass(t, r.classes, "test-group", "user1", "Art", "")
	none, err := r.students.ListWithAliases(ctx, empty.ID)
	require.NoError(t, err)
	assert.Nil(t, none)
}

// TestAliasDeleteCascadesWithStudent verifies aliases are deleted when student is deleted.
func TestAliasDeleteCascadesWithStudent(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
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

	c1 := newTestClass(t, classRepo, "test-group", "user1", "Math", "")
	_ = newTestClass(t, classRepo, "test-group", "user1", "Science", "")

	s1, err := studentRepo.Create(ctx, c1.ID, "Alexander")
	require.NoError(t, err)
	_, err = studentRepo.AddAlias(ctx, s1.ID, "Alex")
	require.NoError(t, err)

	// Alex alias exists in Math, not in Science
	_, err = studentRepo.FindByNameAndClass(ctx, "Alex", "Science · Mon", "user1")
	assert.True(t, errors.Is(err, ErrNotFound), "alias should not match across classes, got: %v", err)

	// Should still work for Math
	id, err := studentRepo.FindByNameAndClass(ctx, "Alex", "Math · Mon", "user1")
	require.NoError(t, err)
	assert.Equal(t, s1.ID, id)
}

// TestMoveStudent_AliasesFollowToTargetClass verifies that Move updates
// student_aliases.class_id along with students.class_id, so no alias row is
// left pointing at the old class.
func TestMoveStudent_AliasesFollowToTargetClass(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c1 := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	c2 := newTestClass(t, r.classes, "test-group", "user1", "Science", "")

	s, err := r.students.Create(ctx, c1.ID, "Alexander")
	require.NoError(t, err)
	a, err := r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)

	dropped, err := r.students.Move(ctx, s.ID, c2.ID)
	require.NoError(t, err, "move")
	assert.Empty(t, dropped)

	got, err := r.students.GetByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, c2.ID, got.ClassID, "student did not move")

	aliases, err := r.students.ListAliases(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, c2.ID, aliases[0].ClassID, "alias did not follow the student to the new class")
	assert.Equal(t, a.Alias, aliases[0].Alias)

	// Old class's alias uniqueness index should no longer see this alias.
	_, err = r.students.FindByNameAndClass(ctx, "Alex", "Math · Mon", "user1")
	assert.True(t, errors.Is(err, ErrNotFound), "alias should no longer resolve in the source class")
}

// TestMoveStudent_NameConflictBlocksMove verifies that moving into a class
// with a colliding canonical name aborts the move and mutates nothing.
func TestMoveStudent_NameConflictBlocksMove(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c1 := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	c2 := newTestClass(t, r.classes, "test-group", "user1", "Science", "")

	s, err := r.students.Create(ctx, c1.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Alex")
	require.NoError(t, err)
	_, err = r.students.Create(ctx, c2.ID, "Alexander")
	require.NoError(t, err)

	_, err = r.students.Move(ctx, s.ID, c2.ID)
	var dupErr *ErrDuplicateStudentName
	require.ErrorAs(t, err, &dupErr, "expected *ErrDuplicateStudentName, got: %v", err)
	assert.Equal(t, "Alexander", dupErr.ConflictName)
	assert.True(t, errors.Is(err, ErrDuplicate), "should still satisfy errors.Is(err, ErrDuplicate)")

	// Nothing mutated: student still in c1, alias still in c1.
	got, err := r.students.GetByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, c1.ID, got.ClassID, "student should not have moved")
	aliases, err := r.students.ListAliases(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, c1.ID, aliases[0].ClassID, "alias should not have moved")
}

// TestMoveStudent_NameConflictWithTargetAlias verifies the collision check
// also catches a target-class alias equal to the moving student's name.
func TestMoveStudent_NameConflictWithTargetAlias(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c1 := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	c2 := newTestClass(t, r.classes, "test-group", "user1", "Science", "")

	s, err := r.students.Create(ctx, c1.ID, "Alex")
	require.NoError(t, err)
	other, err := r.students.Create(ctx, c2.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, other.ID, "Alex")
	require.NoError(t, err)

	_, err = r.students.Move(ctx, s.ID, c2.ID)
	var dupErr *ErrDuplicateStudentName
	require.ErrorAs(t, err, &dupErr, "expected *ErrDuplicateStudentName, got: %v", err)
	assert.Equal(t, "Alexander", dupErr.ConflictName)
}

// TestMoveStudent_CollidingAliasesAreDroppedNotBlocking verifies that an
// alias-only collision does not block the move: the move succeeds, the
// colliding alias is dropped, and it is reported to the caller.
func TestMoveStudent_CollidingAliasesAreDroppedNotBlocking(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c1 := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	c2 := newTestClass(t, r.classes, "test-group", "user1", "Science", "")

	s, err := r.students.Create(ctx, c1.ID, "Emily")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Em")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, s.ID, "Emmy")
	require.NoError(t, err)

	other, err := r.students.Create(ctx, c2.ID, "Emmanuel")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, other.ID, "Em")
	require.NoError(t, err)

	dropped, err := r.students.Move(ctx, s.ID, c2.ID)
	require.NoError(t, err, "move should succeed despite alias collision")
	assert.Equal(t, []string{"Em"}, dropped)

	got, err := r.students.GetByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, c2.ID, got.ClassID, "student should have moved")

	aliases, err := r.students.ListAliases(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1, "only the non-colliding alias should remain")
	assert.Equal(t, "Emmy", aliases[0].Alias)
	assert.Equal(t, c2.ID, aliases[0].ClassID)
}
