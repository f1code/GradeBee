package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleAddAlias_Conflict asserts that POST /students/{id}/aliases returns
// HTTP 409 with error code "alias_conflict" and details.conflictStudentName
// when the alias collides with an existing student or alias in the same class.
func TestHandleAddAlias_Conflict(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	ctx := t.Context()

	c, err := classRepo.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s1, err := studentRepo.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)
	_, err = studentRepo.Create(ctx, c.ID, "Katie")
	require.NoError(t, err)

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{classRepo: classRepo, studentRepo: studentRepo}

	body, err := json.Marshal(map[string]string{"alias": "Katie"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/students/%d/aliases", s1.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	req = req.WithContext(reqCtx)
	rec := httptest.NewRecorder()

	handleAddAlias(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Error   string            `json:"error"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "alias_conflict", resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "Katie", resp.Details["conflictStudentName"])
}

// TestHandleAddAlias_Success asserts that POST /students/{id}/aliases returns
// HTTP 201 with the created alias on success.
func TestHandleAddAlias_Success(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	ctx := t.Context()

	c, err := classRepo.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := studentRepo.Create(ctx, c.ID, "Alexander")
	require.NoError(t, err)

	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()
	serviceDeps = &mockDepsAll{classRepo: classRepo, studentRepo: studentRepo}

	body, err := json.Marshal(map[string]string{"alias": "Alex"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/students/%d/aliases", s.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	req = req.WithContext(reqCtx)
	rec := httptest.NewRecorder()

	handleAddAlias(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp AliasResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Alex", resp.Alias)
	assert.Equal(t, s.ID, resp.StudentID)
}
