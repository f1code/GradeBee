package handler

import (
	"context"
	"errors"
	"testing"
)

// repos is a helper container returned by testDBAndRepos.
type repos struct {
	classes  *ClassRepo
	students *StudentRepo
	notes    *NoteRepo
	reports  *ReportRepo
	examples *ReportExampleRepo
	uploads  *VoiceNoteRepo
}

// testDBAndRepos returns an in-memory SQLite with migrations and all repos.
func testDBAndRepos(t *testing.T) (context.Context, *repos) {
	t.Helper()
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return context.Background(), &repos{
		classes:  &ClassRepo{db: db},
		students: &StudentRepo{db: db},
		notes:    &NoteRepo{db: db},
		reports:  &ReportRepo{db: db},
		examples: &ReportExampleRepo{db: db},
		uploads:  &VoiceNoteRepo{db: db},
	}
}

func TestClassRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	// Create
	c, err := r.classes.Create(ctx, "user1", "Math")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Name != "Math" || c.UserID != "user1" || c.ID == 0 {
		t.Fatalf("unexpected class: %+v", c)
	}

	// List
	list, err := r.classes.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Math" || list[0].StudentCount != 0 {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Duplicate
	_, err = r.classes.Create(ctx, "user1", "Math")
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got: %v", err)
	}

	// Rename
	if err := r.classes.Rename(ctx, "user1", c.ID, "Science"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Rename not found
	if err := r.classes.Rename(ctx, "user1", 999, "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}

	// Delete
	if err := r.classes.Delete(ctx, "user1", c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = r.classes.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after delete")
	}

	// User isolation
	if _, err := r.classes.Create(ctx, "user1", "A"); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := r.classes.Create(ctx, "user2", "B"); err != nil {
		t.Fatalf("create B: %v", err)
	}
	l1, err := r.classes.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list user1: %v", err)
	}
	l2, err := r.classes.List(ctx, "user2")
	if err != nil {
		t.Fatalf("list user2: %v", err)
	}
	if len(l1) != 1 || len(l2) != 1 {
		t.Fatalf("user isolation failed: user1=%d user2=%d", len(l1), len(l2))
	}
}

func TestClassRepo_GetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}
	ctx := context.Background()

	c, err := repo.Create(ctx, "user1", "Math 101")
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "Math 101" {
		t.Errorf("Name = %q, want %q", got.Name, "Math 101")
	}
	if got.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user1")
	}

	_, err = repo.GetByID(ctx, 99999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStudentRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math")
	if err != nil {
		t.Fatalf("create class: %v", err)
	}

	// Create
	s, err := r.students.Create(ctx, c.ID, "Alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.Name != "Alice" || s.ClassID != c.ID {
		t.Fatalf("unexpected student: %+v", s)
	}

	// Duplicate
	_, err = r.students.Create(ctx, c.ID, "Alice")
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got: %v", err)
	}

	// List
	list, err := r.students.List(ctx, c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 student, got %d", len(list))
	}

	// GetByID
	got, err := r.students.GetByID(ctx, s.ID)
	if err != nil || got.Name != "Alice" {
		t.Fatalf("get by id: %v %+v", err, got)
	}

	// BelongsToUser
	ok, err := r.students.BelongsToUser(ctx, s.ID, "user1")
	if err != nil {
		t.Fatalf("belongs: %v", err)
	}
	if !ok {
		t.Fatal("expected belongs to user1")
	}
	ok, err = r.students.BelongsToUser(ctx, s.ID, "user2")
	if err != nil {
		t.Fatalf("belongs: %v", err)
	}
	if ok {
		t.Fatal("should not belong to user2")
	}

	// Move
	c2, err := r.classes.Create(ctx, "user1", "Science")
	if err != nil {
		t.Fatalf("create class2: %v", err)
	}
	if err := r.students.Move(ctx, s.ID, c2.ID); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, err = r.students.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("get after move: %v", err)
	}
	if got.ClassID != c2.ID {
		t.Fatal("move did not update class")
	}

	// Delete
	if err := r.students.Delete(ctx, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = r.students.GetByID(ctx, s.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after delete, got: %v", err)
	}
}

func TestCascadeDelete(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math")
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	s, err := r.students.Create(ctx, c.ID, "Alice")
	if err != nil {
		t.Fatalf("create student: %v", err)
	}
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Good work", Source: "manual"}
	if err := r.notes.Create(ctx, n); err != nil {
		t.Fatalf("create note: %v", err)
	}

	// Delete class should cascade to student and note.
	if err := r.classes.Delete(ctx, "user1", c.ID); err != nil {
		t.Fatalf("delete class: %v", err)
	}

	_, err = r.students.GetByID(ctx, s.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("student should be deleted by cascade")
	}
	_, err = r.notes.GetByID(ctx, n.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("note should be deleted by cascade")
	}
}

func TestNoteRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math")
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	s, err := r.students.Create(ctx, c.ID, "Alice")
	if err != nil {
		t.Fatalf("create student: %v", err)
	}

	// Create
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Great participation", Source: "manual"}
	if err := r.notes.Create(ctx, n); err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.ID == 0 || n.CreatedAt == "" {
		t.Fatalf("fields not populated: %+v", n)
	}

	// Create auto note with transcript
	transcript := "some transcript"
	n2 := &Note{StudentID: s.ID, Date: "2026-01-16", Summary: "Auto note", Transcript: &transcript, Source: "auto"}
	if err := r.notes.Create(ctx, n2); err != nil {
		t.Fatalf("create n2: %v", err)
	}

	// List
	list, err := r.notes.List(ctx, s.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(list))
	}
	// Should be date desc
	if list[0].Date != "2026-01-16" {
		t.Fatalf("wrong order: %s", list[0].Date)
	}

	// Update
	if err := r.notes.Update(ctx, n.ID, "Updated summary"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := r.notes.GetByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Summary != "Updated summary" {
		t.Fatal("summary not updated")
	}

	// ListForStudents
	batch, err := r.notes.ListForStudents(ctx, []int64{s.ID}, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("list for students: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 in batch, got %d", len(batch))
	}

	// Delete
	if err := r.notes.Delete(ctx, n.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = r.notes.GetByID(ctx, n.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found after delete")
	}
}

func TestReportRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math")
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	s, err := r.students.Create(ctx, c.ID, "Alice")
	if err != nil {
		t.Fatalf("create student: %v", err)
	}

	rpt := &Report{StudentID: s.ID, StartDate: "2026-01-01", EndDate: "2026-01-31", HTML: "<h1>Report</h1>"}
	if err := r.reports.Create(ctx, rpt); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rpt.ID == 0 {
		t.Fatal("id not set")
	}

	// List (no HTML)
	list, err := r.reports.List(ctx, s.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != rpt.ID {
		t.Fatalf("list unexpected: %+v", list)
	}

	// GetByID (with HTML)
	got, err := r.reports.GetByID(ctx, rpt.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HTML != "<h1>Report</h1>" {
		t.Fatal("html not returned")
	}

	// Delete
	if err := r.reports.Delete(ctx, rpt.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = r.reports.GetByID(ctx, rpt.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}

func TestReportExampleRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	e, err := r.examples.Create(ctx, "user1", "sample.txt", "Example content")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.ID == 0 {
		t.Fatal("id not set")
	}

	list, err := r.examples.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "sample.txt" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// User isolation
	if _, err := r.examples.Create(ctx, "user2", "other.txt", "other"); err != nil {
		t.Fatalf("create user2: %v", err)
	}
	l1, err := r.examples.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list user1: %v", err)
	}
	l2, err := r.examples.List(ctx, "user2")
	if err != nil {
		t.Fatalf("list user2: %v", err)
	}
	if len(l1) != 1 || len(l2) != 1 {
		t.Fatal("user isolation failed")
	}

	// Delete
	if err := r.examples.Delete(ctx, "user1", e.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = r.examples.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatal("expected empty after delete")
	}

	// Delete wrong user
	e2, err := r.examples.Create(ctx, "user1", "x.txt", "x")
	if err != nil {
		t.Fatalf("create e2: %v", err)
	}
	err = r.examples.Delete(ctx, "user2", e2.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("should not delete other user's example")
	}

	// Update
	updated, err := r.examples.Update(ctx, "user1", e2.ID, "renamed.txt", "new content")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed.txt" || updated.Content != "new content" {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	// Update wrong user
	_, err = r.examples.Update(ctx, "user2", e2.ID, "hack", "hack")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("should not update other user's example, got: %v", err)
	}
}

func TestUploadRepo_CRUD(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	u, err := r.uploads.Create(ctx, "user1", "audio.mp3", "/data/uploads/abc.mp3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ProcessedAt != nil {
		t.Fatal("should not be processed yet")
	}

	// MarkProcessed
	if err := r.uploads.MarkProcessed(ctx, u.ID); err != nil {
		t.Fatalf("mark processed: %v", err)
	}
	got, err := r.uploads.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProcessedAt == nil {
		t.Fatal("should be processed")
	}

	// ListStale — use a future cutoff
	stale, err := r.uploads.ListStale(ctx, "2099-01-01T00:00:00.000Z")
	if err != nil {
		t.Fatalf("list stale: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale, got %d", len(stale))
	}

	// ListStale — past cutoff
	stale, err = r.uploads.ListStale(ctx, "2000-01-01T00:00:00.000Z")
	if err != nil {
		t.Fatalf("list stale past: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected 0 stale, got %d", len(stale))
	}

	// Delete
	if err := r.uploads.Delete(ctx, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = r.uploads.GetByID(ctx, u.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}

func TestClassStudentCount(t *testing.T) {
	ctx, r := testDBAndRepos(t)

	c, err := r.classes.Create(ctx, "user1", "Math")
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	if _, err := r.students.Create(ctx, c.ID, "Alice"); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := r.students.Create(ctx, c.ID, "Bob"); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	list, err := r.classes.List(ctx, "user1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].StudentCount != 2 {
		t.Fatalf("expected count 2, got %+v", list)
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second run should be idempotent: %v", err)
	}
}
