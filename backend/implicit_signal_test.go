package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupImplicitSignalDeps wires a real in-memory DB as serviceDeps including all repos.
// Returns the repos and a background context.
func setupImplicitSignalDeps(t *testing.T) (context.Context, *repos) {
	t.Helper()
	ctx, r := testDBAndRepos(t)
	db := r.notes.db
	serviceDeps = &mockDepsAll{
		db:           db,
		classRepo:    r.classes,
		studentRepo:  r.students,
		noteRepo:     r.notes,
		reportRepo:   r.reports,
		feedbackRepo: r.feedback,
	}
	t.Cleanup(func() { serviceDeps = nil })
	return ctx, r
}

func TestImplicitSignal_EditAutoNote(t *testing.T) {
	ctx, r := setupImplicitSignalDeps(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Original summary", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	body, err := json.Marshal(map[string]string{"summary": "Edited summary"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
	req = withClerkUser(req, "user1")

	rec := httptest.NewRecorder()
	handleUpdateNote(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	// Verify implicit 'edited' feedback row
	list, err := r.feedback.ListByArtifact(ctx, "note", n.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "edited", list[0].Signal)
	assert.Equal(t, "down", list[0].Rating)
	require.NotNil(t, list[0].PreviousValue)
	assert.Equal(t, "Original summary", *list[0].PreviousValue)
	assert.Nil(t, list[0].Comment)
}

func TestImplicitSignal_EditAutoNote_NoChangeNoPowder(t *testing.T) {
	ctx, r := setupImplicitSignalDeps(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Same summary", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	// Edit with same content — no feedback row expected
	body, err := json.Marshal(map[string]string{"summary": "Same summary"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()
	handleUpdateNote(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	list, err := r.feedback.ListByArtifact(ctx, "note", n.ID)
	require.NoError(t, err)
	assert.Empty(t, list, "no feedback row when summary unchanged")
}

func TestImplicitSignal_EditManualNote_NoFeedback(t *testing.T) {
	ctx, r := setupImplicitSignalDeps(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Manual note", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, n))

	body, err := json.Marshal(map[string]string{"summary": "Updated manual"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()
	handleUpdateNote(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	list, err := r.feedback.ListByArtifact(ctx, "note", n.ID)
	require.NoError(t, err)
	assert.Empty(t, list, "manual note edits should not generate feedback rows")
}

func TestImplicitSignal_DeleteAutoNote(t *testing.T) {
	ctx, r := setupImplicitSignalDeps(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Auto note to delete", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))
	noteID := n.ID

	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/notes/%d", noteID), http.NoBody)
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()
	handleDeleteNote(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Note is gone but feedback row should exist (dangling artifact_id by design)
	list, err := r.feedback.ListByArtifact(ctx, "note", noteID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "deleted", list[0].Signal)
	assert.Equal(t, "down", list[0].Rating)
	require.NotNil(t, list[0].PreviousValue)
	assert.Equal(t, "Auto note to delete", *list[0].PreviousValue)
}

func TestImplicitSignal_DeleteManualNote_NoFeedback(t *testing.T) {
	ctx, r := setupImplicitSignalDeps(t)

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Manual to delete", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, n))
	noteID := n.ID

	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/notes/%d", noteID), http.NoBody)
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()
	handleDeleteNote(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	list, err := r.feedback.ListByArtifact(ctx, "note", noteID)
	require.NoError(t, err)
	assert.Empty(t, list, "manual note deletes should not generate feedback rows")
}
