package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDBRoster_Students asserts the roster read for user A — one query for
// all classes — is exactly A's classes in position order, each holding its
// students by name with their aliases by value (sorted, non-nil when empty),
// and none of user B's. B's class is asserted present in B's own roster so
// the absence from A's can fail (see ARCHITECTURE.md, "Assertions must be
// able to fail").
func TestDBRoster_Students(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	// User A: "Science" created before "Math" so position order (creation)
	// differs from name order and the assertion pins which one wins.
	science := newTestClass(t, r.classes, "test-group", "userA", "Science", "14:10")
	math := newTestClass(t, r.classes, "test-group", "userA", "Math", "")

	carl, err := r.students.Create(ctx, science.ID, "Carl")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, carl.ID, "Charlie")
	require.NoError(t, err)

	// Students and aliases inserted out of order so ordering is the query's.
	_, err = r.students.Create(ctx, math.ID, "Beatrice")
	require.NoError(t, err)
	alexander, err := r.students.Create(ctx, math.ID, "Alexander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, alexander.ID, "Xander")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, alexander.ID, "Alex")
	require.NoError(t, err)

	// User B: same level name as one of A's, with a student and alias.
	bMath := newTestClass(t, r.classes, "test-group", "userB", "Math", "")
	dora, err := r.students.Create(ctx, bMath.ID, "Dora")
	require.NoError(t, err)
	_, err = r.students.AddAlias(ctx, dora.ID, "Dee")
	require.NoError(t, err)

	got, err := newDBRoster(r.classes, r.students, "userA").Students(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ClassGroup{
		{Name: "Science · Mon · 14:10", Students: []ClassStudent{
			{Name: "Carl", Aliases: []string{"Charlie"}},
		}},
		{Name: "Math · Mon", Students: []ClassStudent{
			{Name: "Alexander", Aliases: []string{"Alex", "Xander"}},
			{Name: "Beatrice", Aliases: []string{}},
		}},
	}, got)

	// Absence of B's data from A's roster, paired with its presence in B's.
	for _, cg := range got {
		for _, s := range cg.Students {
			assert.NotEqual(t, "Dora", s.Name)
			assert.NotContains(t, s.Aliases, "Dee")
		}
	}
	gotB, err := newDBRoster(r.classes, r.students, "userB").Students(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ClassGroup{
		{Name: "Math · Mon", Students: []ClassStudent{
			{Name: "Dora", Aliases: []string{"Dee"}},
		}},
	}, gotB)
}

// TestDBRoster_Students_EmptyClass asserts a class with no students still
// appears, with a non-nil empty Students slice, alongside a populated one.
func TestDBRoster_Students_EmptyClass(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	math := newTestClass(t, r.classes, "test-group", "userA", "Math", "")
	newTestClass(t, r.classes, "test-group", "userA", "Art", "")
	_, err := r.students.Create(ctx, math.ID, "Alexander")
	require.NoError(t, err)

	got, err := newDBRoster(r.classes, r.students, "userA").Students(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ClassGroup{
		{Name: "Math · Mon", Students: []ClassStudent{{Name: "Alexander", Aliases: []string{}}}},
		{Name: "Art · Mon", Students: []ClassStudent{}},
	}, got)
}

// TestDBRoster_Students_NoClasses asserts a user with no classes gets a nil
// roster (not an empty slice) — what the extraction prompt builder receives.
func TestDBRoster_Students_NoClasses(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	newTestClass(t, r.classes, "test-group", "userB", "Math", "")

	got, err := newDBRoster(r.classes, r.students, "userA").Students(ctx)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestDBRoster_ClassNames asserts the names are the user's own classes'
// display names in position order, and an empty non-nil slice for a user
// with none.
func TestDBRoster_ClassNames(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	newTestClass(t, r.classes, "test-group", "userA", "Science", "14:10")
	newTestClass(t, r.classes, "test-group", "userA", "Math", "")
	newTestClass(t, r.classes, "test-group", "userB", "Art", "")

	got, err := newDBRoster(r.classes, r.students, "userA").ClassNames(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"Science · Mon · 14:10", "Math · Mon"}, got)

	gotB, err := newDBRoster(r.classes, r.students, "userB").ClassNames(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"Art · Mon"}, gotB)

	none, err := newDBRoster(r.classes, r.students, "userC").ClassNames(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, none)
}
