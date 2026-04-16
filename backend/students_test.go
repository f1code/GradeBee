package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
)

func TestHandleGetStudents_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}

	// Seed data
	c1, err := classRepo.Create(t.Context(), "test-user", "5A", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := classRepo.Create(t.Context(), "test-user", "5B", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), c1.ID, "Emma"); err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), c1.ID, "Liam"); err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), c2.ID, "Noah"); err != nil {
		t.Fatal(err)
	}

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{
		classRepo:   classRepo,
		studentRepo: studentRepo,
	}

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	ctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "test-user"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handleGetStudents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp StudentsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Classes) != 2 {
		t.Errorf("got %d classes, want 2", len(resp.Classes))
	}
	if resp.Classes[0].Name != "5A" {
		t.Errorf("first class = %q, want 5A", resp.Classes[0].Name)
	}
	if len(resp.Classes[0].Students) != 2 {
		t.Errorf("5A students = %d, want 2", len(resp.Classes[0].Students))
	}
}

func TestHandleGetStudents_Empty(t *testing.T) {
	db := setupTestDB(t)

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{
		classRepo:   &ClassRepo{db: db},
		studentRepo: &StudentRepo{db: db},
	}

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	ctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "test-user"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handleGetStudents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp StudentsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Classes) != 0 {
		t.Errorf("got %d classes, want 0", len(resp.Classes))
	}
}

// setupTestDB opens an in-memory SQLite DB with migrations applied.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestListClassNames(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}

	for _, args := range [][2]string{
		{"Alpha", ""},
		{"Beta", "AM"},
		{"Alpha", "PM"},
	} {
		if _, err := classRepo.Create(t.Context(), "test-user", args[0], args[1]); err != nil {
			t.Fatal(err)
		}
	}

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{classRepo: classRepo, studentRepo: &StudentRepo{db: db}}

	req := httptest.NewRequest(http.MethodGet, "/classes/class-names", http.NoBody)
	ctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "test-user"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handleListClassNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string][]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := resp["classNames"]
	if len(names) != 2 {
		t.Errorf("got %v, want 2 distinct names", names)
	}
}
