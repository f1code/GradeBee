package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeClerkUser injects a fake Clerk session into a context so
// userIDFromRequest succeeds without a real JWT.
func withFakeClerkUser(ctx context.Context, userID string) context.Context {
	return clerk.ContextWithSessionClaims(ctx, &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
}

// withClerkUser returns a new request with a fake Clerk session for userID.
func withClerkUser(req *http.Request, userID string) *http.Request {
	return req.WithContext(withFakeClerkUser(req.Context(), userID))
}

func TestHandleSubmitFeedback_ReportThumbsDown(t *testing.T) {
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

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	rpt := &Report{StudentID: s.ID, StartDate: "2026-01-01", EndDate: "2026-01-31", HTML: "<p>r</p>"}
	require.NoError(t, r.reports.Create(ctx, rpt))

	comment := "tone was off"
	body, err := json.Marshal(submitFeedbackRequest{
		ArtifactType: "report",
		ArtifactID:   rpt.ID,
		Rating:       "down",
		Comment:      &comment,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewReader(body))
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()

	handleSubmitFeedback(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	// Verify feedback row was created
	list, err := r.feedback.ListByArtifact(ctx, "report", rpt.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "down", list[0].Rating)
	assert.Equal(t, "explicit", list[0].Signal)
	require.NotNil(t, list[0].Comment)
	assert.Equal(t, comment, *list[0].Comment)
}

func TestHandleSubmitFeedback_NoteThumbsUp(t *testing.T) {
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

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Good work", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	body, err := json.Marshal(submitFeedbackRequest{
		ArtifactType: "note",
		ArtifactID:   n.ID,
		Rating:       "up",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewReader(body))
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()

	handleSubmitFeedback(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	list, err := r.feedback.ListByArtifact(ctx, "note", n.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "up", list[0].Rating)
}

func TestHandleSubmitFeedback_WrongUser(t *testing.T) {
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

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	rpt := &Report{StudentID: s.ID, StartDate: "2026-01-01", EndDate: "2026-01-31", HTML: "<p>r</p>"}
	require.NoError(t, r.reports.Create(ctx, rpt))

	body, err := json.Marshal(submitFeedbackRequest{
		ArtifactType: "report",
		ArtifactID:   rpt.ID,
		Rating:       "down",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewReader(body))
	req = withClerkUser(req, "user2") // different user
	rec := httptest.NewRecorder()

	handleSubmitFeedback(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	// No feedback row should be created
	list, err := r.feedback.ListByArtifact(ctx, "report", rpt.ID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestHandleSubmitFeedback_InvalidArtifactType(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	_ = ctx
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

	body, err := json.Marshal(map[string]any{"artifact_type": "class", "artifact_id": 1, "rating": "down"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewReader(body))
	req = withClerkUser(req, "user1")
	rec := httptest.NewRecorder()

	handleSubmitFeedback(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
