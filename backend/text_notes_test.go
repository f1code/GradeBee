package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
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

	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty text: got status %d, want 400", rec.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "text is required" {
		t.Errorf("empty text: got error %q, want %q", resp["error"], "text is required")
	}
}

func TestHandleTextNotesUpload_TooLarge(t *testing.T) {
	big := strings.Repeat("x", maxTextSize+1)
	body, err := json.Marshal(textNotesRequest{Text: big})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/text-notes/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user-1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()

	handleTextNotesUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("too large: got status %d, want 400", rec.Code)
	}
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

	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid json: got status %d, want 400", rec.Code)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/text-notes/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user-1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()

	handleTextNotesUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("happy path: got status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp UploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.UploadID == 0 {
		t.Error("expected non-zero upload ID")
	}
	if resp.FileName != "pasted-text" {
		t.Errorf("got fileName %q, want %q", resp.FileName, "pasted-text")
	}

	// Verify the job was published with the transcript.
	jobs, err := queue.ListJobs(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Transcript != "Alice did great today" {
		t.Errorf("job transcript = %q, want %q", jobs[0].Transcript, "Alice did great today")
	}
	if jobs[0].Source != "text" {
		t.Errorf("job source = %q, want %q", jobs[0].Source, "text")
	}
}
