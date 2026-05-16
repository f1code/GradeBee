package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repos is a helper container returned by testDBAndRepos.
type repos struct {
	classes  *ClassRepo
	students *StudentRepo
	notes    *NoteRepo
	reports  *ReportRepo
	examples *ReportExampleRepo
	voiceNotes *VoiceNoteRepo
}

// testDBAndRepos returns an in-memory SQLite with migrations and all repos.
func testDBAndRepos(t *testing.T) (context.Context, *repos) {
	t.Helper()
	db, err := OpenDB(":memory:")
	require.NoError(t, err, "open test db")
	require.NoError(t, RunMigrations(db), "run migrations")
	t.Cleanup(func() { db.Close() })
	return context.Background(), &repos{
		classes:  &ClassRepo{db: db},
		students: &StudentRepo{db: db},
		notes:    &NoteRepo{db: db},
		reports:  &ReportRepo{db: db},
		examples: &ReportExampleRepo{db: db},
		voiceNotes: &VoiceNoteRepo{db: db},
	}
}

func TestClassRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	// Create
	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err, "create")
	assert.Equal(t, "Math", c.Name)
	assert.Equal(t, "user1", c.UserID)
	assert.NotZero(t, c.ID)

	// List
	list, err := r.classes.List(ctx, "user1")
	require.NoError(t, err, "list")
	require.Len(t, list, 1)
	assert.Equal(t, "Math", list[0].Name)
	assert.Equal(t, 0, list[0].StudentCount)

	// Duplicate
	_, err = r.classes.Create(ctx, "user1", "Math", "")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate, got: %v", err)

	// Rename
	require.NoError(t, r.classes.Update(ctx, "user1", c.ID, "Science", ""), "rename")

	// Rename not found
	err = r.classes.Update(ctx, "user1", 999, "X", "")
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got: %v", err)

	// Delete
	require.NoError(t, r.classes.Delete(ctx, "user1", c.ID), "delete")
	list, err = r.classes.List(ctx, "user1")
	require.NoError(t, err, "list after delete")
	assert.Empty(t, list)

	// User isolation
	_, err = r.classes.Create(ctx, "user1", "A", "")
	require.NoError(t, err, "create A")
	_, err = r.classes.Create(ctx, "user2", "B", "")
	require.NoError(t, err, "create B")
	l1, err := r.classes.List(ctx, "user1")
	require.NoError(t, err, "list user1")
	l2, err := r.classes.List(ctx, "user2")
	require.NoError(t, err, "list user2")
	assert.Len(t, l1, 1, "user isolation failed for user1")
	assert.Len(t, l2, 1, "user isolation failed for user2")
}

func TestClassRepo_GetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}
	ctx := context.Background()

	c, err := repo.Create(ctx, "user1", "Math 101", "")
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, c.ID)
	require.NoError(t, err, "GetByID")
	assert.Equal(t, "Math 101", got.Name)
	assert.Equal(t, "user1", got.UserID)

	_, err = repo.GetByID(ctx, 99999)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestStudentRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err, "create class")

	// Create
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err, "create")
	assert.Equal(t, "Alice", s.Name)
	assert.Equal(t, c.ID, s.ClassID)

	// Duplicate
	_, err = r.students.Create(ctx, c.ID, "Alice")
	assert.True(t, errors.Is(err, ErrDuplicate), "expected ErrDuplicate, got: %v", err)

	// List
	list, err := r.students.List(ctx, c.ID)
	require.NoError(t, err, "list")
	assert.Len(t, list, 1)

	// GetByID
	got, err := r.students.GetByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", got.Name)

	// BelongsToUser
	ok, err := r.students.BelongsToUser(ctx, s.ID, "user1")
	require.NoError(t, err, "belongs")
	assert.True(t, ok)
	ok, err = r.students.BelongsToUser(ctx, s.ID, "user2")
	require.NoError(t, err, "belongs")
	assert.False(t, ok)

	// Move
	c2, err := r.classes.Create(ctx, "user1", "Science", "")
	require.NoError(t, err, "create class2")
	require.NoError(t, r.students.Move(ctx, s.ID, c2.ID), "move")
	got, err = r.students.GetByID(ctx, s.ID)
	require.NoError(t, err, "get after move")
	assert.Equal(t, c2.ID, got.ClassID, "move did not update class")

	// Delete
	require.NoError(t, r.students.Delete(ctx, s.ID), "delete")
	_, err = r.students.GetByID(ctx, s.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "expected not found after delete, got: %v", err)
}

func TestCascadeDelete(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err, "create class")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err, "create student")
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Good work", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, n), "create note")

	// Delete class should cascade to student and note.
	require.NoError(t, r.classes.Delete(ctx, "user1", c.ID), "delete class")

	_, err = r.students.GetByID(ctx, s.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "student should be deleted by cascade")
	_, err = r.notes.GetByID(ctx, n.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "note should be deleted by cascade")
}

func TestNoteRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err, "create class")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err, "create student")

	// Create
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Great participation", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, n), "create")
	assert.NotZero(t, n.ID)
	assert.NotEmpty(t, n.CreatedAt)

	// Create auto note with transcript
	transcript := "some transcript"
	n2 := &Note{StudentID: s.ID, Date: "2026-01-16", Summary: "Auto note", Transcript: &transcript, Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n2), "create n2")

	// List
	list, err := r.notes.List(ctx, s.ID)
	require.NoError(t, err, "list")
	assert.Len(t, list, 2)
	// Should be date desc
	assert.Equal(t, "2026-01-16", list[0].Date, "wrong order")

	// Update
	require.NoError(t, r.notes.Update(ctx, n.ID, "Updated summary"), "update")
	got, err := r.notes.GetByID(ctx, n.ID)
	require.NoError(t, err, "get")
	assert.Equal(t, "Updated summary", got.Summary)

	// ListForStudents
	batch, err := r.notes.ListForStudents(ctx, []int64{s.ID}, "2026-01-01", "2026-12-31")
	require.NoError(t, err, "list for students")
	assert.Len(t, batch, 2)

	// Delete
	require.NoError(t, r.notes.Delete(ctx, n.ID), "delete")
	_, err = r.notes.GetByID(ctx, n.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "expected not found after delete")
}

func TestReportRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err, "create class")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err, "create student")

	rpt := &Report{StudentID: s.ID, StartDate: "2026-01-01", EndDate: "2026-01-31", HTML: "<h1>Report</h1>"}
	require.NoError(t, r.reports.Create(ctx, rpt), "create")
	assert.NotZero(t, rpt.ID)

	// List (no HTML)
	list, err := r.reports.List(ctx, s.ID)
	require.NoError(t, err, "list")
	require.Len(t, list, 1)
	assert.Equal(t, rpt.ID, list[0].ID)

	// GetByID (with HTML)
	got, err := r.reports.GetByID(ctx, rpt.ID)
	require.NoError(t, err, "get")
	assert.Equal(t, "<h1>Report</h1>", got.HTML)

	// Delete
	require.NoError(t, r.reports.Delete(ctx, rpt.ID), "delete")
	_, err = r.reports.GetByID(ctx, rpt.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "expected not found")
}

func TestReportExampleRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	e, err := r.examples.Create(ctx, "user1", "sample.txt", "Example content")
	require.NoError(t, err, "create")
	assert.NotZero(t, e.ID)

	list, err := r.examples.List(ctx, "user1")
	require.NoError(t, err, "list")
	require.Len(t, list, 1)
	assert.Equal(t, "sample.txt", list[0].Name)

	// User isolation
	_, err = r.examples.Create(ctx, "user2", "other.txt", "other")
	require.NoError(t, err, "create user2")
	l1, err := r.examples.List(ctx, "user1")
	require.NoError(t, err, "list user1")
	l2, err := r.examples.List(ctx, "user2")
	require.NoError(t, err, "list user2")
	assert.Len(t, l1, 1, "user isolation failed")
	assert.Len(t, l2, 1, "user isolation failed")

	// Delete
	require.NoError(t, r.examples.Delete(ctx, "user1", e.ID), "delete")
	list, err = r.examples.List(ctx, "user1")
	require.NoError(t, err, "list after delete")
	assert.Empty(t, list)

	// Delete wrong user
	e2, err := r.examples.Create(ctx, "user1", "x.txt", "x")
	require.NoError(t, err, "create e2")
	err = r.examples.Delete(ctx, "user2", e2.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "should not delete other user's example")

	// Update
	updated, err := r.examples.Update(ctx, "user1", e2.ID, "renamed.txt", "new content")
	require.NoError(t, err, "update")
	assert.Equal(t, "renamed.txt", updated.Name)
	assert.Equal(t, "new content", updated.Content)

	// Update wrong user
	_, err = r.examples.Update(ctx, "user2", e2.ID, "hack", "hack")
	assert.True(t, errors.Is(err, ErrNotFound), "should not update other user's example, got: %v", err)
}

func TestVoiceNoteRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	u, err := r.voiceNotes.Create(ctx, "user1", "audio.mp3", "/data/uploads/abc.mp3")
	require.NoError(t, err, "create")
	assert.Nil(t, u.ProcessedAt, "should not be processed yet")

	// MarkProcessed
	require.NoError(t, r.voiceNotes.MarkProcessed(ctx, u.ID), "mark processed")
	got, err := r.voiceNotes.GetByID(ctx, u.ID)
	require.NoError(t, err, "get")
	assert.NotNil(t, got.ProcessedAt, "should be processed")

	// ListStale — use a future cutoff
	stale, err := r.voiceNotes.ListStale(ctx, "2099-01-01T00:00:00.000Z")
	require.NoError(t, err, "list stale")
	assert.Len(t, stale, 1)

	// ListStale — past cutoff
	stale, err = r.voiceNotes.ListStale(ctx, "2000-01-01T00:00:00.000Z")
	require.NoError(t, err, "list stale past")
	assert.Empty(t, stale)

	// Delete
	require.NoError(t, r.voiceNotes.Delete(ctx, u.ID), "delete")
	_, err = r.voiceNotes.GetByID(ctx, u.ID)
	assert.True(t, errors.Is(err, ErrNotFound), "expected not found")
}

func TestClassStudentCount(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err, "create class")
	_, err = r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err, "create alice")
	_, err = r.students.Create(ctx, c.ID, "Bob")
	require.NoError(t, err, "create bob")

	list, err := r.classes.List(ctx, "user1")
	require.NoError(t, err, "list")
	require.Len(t, list, 1)
	assert.Equal(t, 2, list[0].StudentCount)
}

func TestMigrationsIdempotent(t *testing.T) {
	db, err := OpenDB(":memory:")
	require.NoError(t, err, "open")
	defer db.Close()

	require.NoError(t, RunMigrations(db), "first run")
	require.NoError(t, RunMigrations(db), "second run should be idempotent")
}
