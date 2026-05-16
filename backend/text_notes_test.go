package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTextNotesUpload_EmptyText(t *testing.T) {
	body := `{"text":""}`
	req := httptest.NewRequest(http.MethodPost, "/text-notes/upload", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Simulate authenticated user via Clerk claims.
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user-1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()

	handleTextNotesUpload(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "empty text: unexpected status")
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp), "decode response")
	assert.Equal(t, "text is required", resp["error"])
}

func TestHandleTextNotesUpload_TooLarge(t *testing.T) {
	big := strings.Repeat("x", maxTextSize+1)
	body, err := json.Marshal(textNotesRequest{Text: big})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/text-notes/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user-1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()

	handleTextNotesUpload(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "too large: unexpected status")
}

func TestHandleTextNotesUpload_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/text-notes/upload", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user-1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()

	handleTextNotesUpload(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "invalid json: unexpected status")
}

func TestHandleTextNotesUpload_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	queue := newTestQueue(t)

	oldDeps := serviceDeps
	serviceDeps = &mockDepsAll{
		db:             db,
		voiceNoteRepo:  &VoiceNoteRepo{db: db},
		voiceNoteQueue: queue,
	}
	t.Cleanup(func() { serviceDeps = oldDeps })

	body, err := json.Marshal(textNotesRequest{Text: "Alice did great today"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/text-notes/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user-1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()

	handleTextNotesUpload(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "happy path: body: %s", rec.Body.String())

	var resp UploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp), "decode response")
	assert.NotZero(t, resp.UploadID, "expected non-zero upload ID")
	assert.Equal(t, "pasted-text", resp.FileName)

	// Verify the job was published with the transcript.
	jobs, err := queue.ListJobs(t.Context(), "user-1")
	require.NoError(t, err, "list jobs")
	require.Len(t, jobs, 1)
	assert.Equal(t, "Alice did great today", jobs[0].Transcript)
	assert.Equal(t, "text", jobs[0].Source)
}
