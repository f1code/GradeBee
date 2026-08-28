package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB opens an in-memory SQLite DB with migrations applied.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	require.NoError(t, RunMigrations(db))
	return db
}

func putUpdateStudentRequest(t *testing.T, studentID int64, body map[string]any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/students/%d", studentID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	return req.WithContext(reqCtx)
}

// TestHandleUpdateStudent_MoveConflict asserts that PUT /students/{id} with a
// classId returns HTTP 409 (code "student_name_conflict") naming the
// conflicting student when the move collides with a name already in the
// target class, and that the response has no droppedAliases key.
func TestHandleUpdateStudent_MoveConflict(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	ctx := t.Context()

	source := newTestClass(t, classRepo, "test-group", "user1", "Math", "")
	target := newTestClass(t, classRepo, "test-group", "user1", "Science", "")
	s, err := studentRepo.Create(ctx, source.ID, "Alexander")
	require.NoError(t, err)
	_, err = studentRepo.Create(ctx, target.ID, "Alexander")
	require.NoError(t, err)

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{classRepo: classRepo, studentRepo: studentRepo}

	req := putUpdateStudentRequest(t, s.ID, map[string]any{"classId": target.ID})
	rec := httptest.NewRecorder()

	handleUpdateStudent(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Error   string            `json:"error"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "student_name_conflict", resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "Alexander", resp.Details["conflictStudentName"])

	// Nothing mutated.
	got, err := studentRepo.GetByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, source.ID, got.ClassID, "student should not have moved")
}

// TestHandleUpdateStudent_MoveSuccessWithDroppedAliases asserts that a
// successful move returns HTTP 200 with droppedAliases listing any aliases
// dropped for colliding with the target class.
func TestHandleUpdateStudent_MoveSuccessWithDroppedAliases(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	ctx := t.Context()

	source := newTestClass(t, classRepo, "test-group", "user1", "Math", "")
	target := newTestClass(t, classRepo, "test-group", "user1", "Science", "")
	s, err := studentRepo.Create(ctx, source.ID, "Emily")
	require.NoError(t, err)
	_, err = studentRepo.AddAlias(ctx, s.ID, "Em")
	require.NoError(t, err)
	other, err := studentRepo.Create(ctx, target.ID, "Emmanuel")
	require.NoError(t, err)
	_, err = studentRepo.AddAlias(ctx, other.ID, "Em")
	require.NoError(t, err)

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{classRepo: classRepo, studentRepo: studentRepo}

	req := putUpdateStudentRequest(t, s.ID, map[string]any{"classId": target.ID})
	rec := httptest.NewRecorder()

	handleUpdateStudent(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Status         string   `json:"status"`
		DroppedAliases []string `json:"droppedAliases"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "updated", resp.Status)
	assert.Equal(t, []string{"Em"}, resp.DroppedAliases)

	got, err := studentRepo.GetByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, target.ID, got.ClassID, "student should have moved")
}

// TestHandleUpdateStudent_MoveSuccessOmitsDroppedAliasesWhenEmpty asserts the
// droppedAliases key is absent from the response on a clean move.
func TestHandleUpdateStudent_MoveSuccessOmitsDroppedAliasesWhenEmpty(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	ctx := t.Context()

	source := newTestClass(t, classRepo, "test-group", "user1", "Math", "")
	target := newTestClass(t, classRepo, "test-group", "user1", "Science", "")
	s, err := studentRepo.Create(ctx, source.ID, "Alexander")
	require.NoError(t, err)

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{classRepo: classRepo, studentRepo: studentRepo}

	req := putUpdateStudentRequest(t, s.ID, map[string]any{"classId": target.ID})
	rec := httptest.NewRecorder()

	handleUpdateStudent(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var raw map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))
	_, hasDropped := raw["droppedAliases"]
	assert.False(t, hasDropped, "droppedAliases should be omitted on a clean move")
}
