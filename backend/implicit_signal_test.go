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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Original summary", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	body, err := json.Marshal(map[string]string{"summary": "Edited summary"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
	req.SetPathValue("id", itoa(n.ID))
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Same summary", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	// Edit with same content — no feedback row expected
	body, err := json.Marshal(map[string]string{"summary": "Same summary"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
	req.SetPathValue("id", itoa(n.ID))
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Manual note", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, n))

	body, err := json.Marshal(map[string]string{"summary": "Updated manual"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
	req.SetPathValue("id", itoa(n.ID))
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Auto note to delete", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))
	noteID := n.ID

	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/notes/%d", noteID), http.NoBody)
	req.SetPathValue("id", itoa(noteID))
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

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Manual to delete", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, n))
	noteID := n.ID

	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/notes/%d", noteID), http.NoBody)
	req.SetPathValue("id", itoa(noteID))
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()
	handleDeleteNote(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	list, err := r.feedback.ListByArtifact(ctx, "note", noteID)
	require.NoError(t, err)
	assert.Empty(t, list, "manual note deletes should not generate feedback rows")
}

// A reviewed note is model-written: the teacher supplied only the class, the
// model wrote the sentence, and editing that sentence away is the same signal
// about the same text as editing an auto note away. This is the behaviour
// isModelWritten widened, so it is pinned in both directions — manual notes,
// which the teacher wrote themselves, must stay silent.
func TestImplicitSignal_EditReviewedNote(t *testing.T) {
	for _, tc := range []struct {
		source   string
		wantRows int
	}{
		{NoteSourceAuto, 1},
		{NoteSourceReviewed, 1},
		{NoteSourceManual, 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			ctx, r := setupImplicitSignalDeps(t)

			c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
			s, err := r.students.Create(ctx, c.ID, "Alice")
			require.NoError(t, err)
			n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Original summary", Source: tc.source}
			require.NoError(t, r.notes.Create(ctx, n))

			body, err := json.Marshal(map[string]string{"summary": "Edited summary"})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut,
				fmt.Sprintf("/notes/%d", n.ID), bytes.NewReader(body))
			req.SetPathValue("id", itoa(n.ID))
			req = withClerkUser(req, "user1")

			rec := httptest.NewRecorder()
			handleUpdateNote(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

			list, err := r.feedback.ListByArtifact(ctx, "note", n.ID)
			require.NoError(t, err)
			require.Len(t, list, tc.wantRows)
			if tc.wantRows == 0 {
				return
			}
			assert.Equal(t, "edited", list[0].Signal)
			assert.Equal(t, "down", list[0].Rating)
			require.NotNil(t, list[0].PreviousValue)
			assert.Equal(t, "Original summary", *list[0].PreviousValue)
		})
	}
}

func TestImplicitSignal_DeleteReviewedNote(t *testing.T) {
	for _, tc := range []struct {
		source   string
		wantRows int
	}{
		{NoteSourceAuto, 1},
		{NoteSourceReviewed, 1},
		{NoteSourceManual, 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			ctx, r := setupImplicitSignalDeps(t)

			c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
			s, err := r.students.Create(ctx, c.ID, "Alice")
			require.NoError(t, err)
			n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Note to delete", Source: tc.source}
			require.NoError(t, r.notes.Create(ctx, n))
			noteID := n.ID

			req := httptest.NewRequest(http.MethodDelete,
				fmt.Sprintf("/notes/%d", noteID), http.NoBody)
			req.SetPathValue("id", itoa(noteID))
			req = withClerkUser(req, "user1")
			rec := httptest.NewRecorder()
			handleDeleteNote(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)

			list, err := r.feedback.ListByArtifact(ctx, "note", noteID)
			require.NoError(t, err)
			require.Len(t, list, tc.wantRows)
			if tc.wantRows == 0 {
				return
			}
			assert.Equal(t, "deleted", list[0].Signal)
			require.NotNil(t, list[0].PreviousValue)
			assert.Equal(t, "Note to delete", *list[0].PreviousValue)
		})
	}
}
