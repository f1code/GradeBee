package handler

import (
	"database/sql"
	"encoding/json"
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

func TestListClassNames(t *testing.T) {
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}

	for _, args := range [][2]string{
		{"Alpha", ""},
		{"Beta", "AM"},
		{"Alpha", "PM"},
	} {
		_, err := classRepo.Create(t.Context(), "test-user", args[0], args[1])
		require.NoError(t, err)
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

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp map[string][]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp), "decode failed")
	names := resp["classNames"]
	assert.Len(t, names, 2, "got %v, want 2 distinct names", names)
}
