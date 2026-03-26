package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

func clerkCtx(r *http.Request, userID string) *http.Request {
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
	return r.WithContext(ctx)
}

func TestJobList_GroupsByStatus(t *testing.T) {
	queue := newStubUploadQueue()
	now := time.Now()

	queue.jobs[kvKey("u1", "f1")] = UploadJob{UserID: "u1", FileID: "f1", Status: JobStatusQueued, CreatedAt: now}
	queue.jobs[kvKey("u1", "f2")] = UploadJob{UserID: "u1", FileID: "f2", Status: JobStatusTranscribing, CreatedAt: now.Add(-1 * time.Minute)}
	queue.jobs[kvKey("u1", "f3")] = UploadJob{UserID: "u1", FileID: "f3", Status: JobStatusDone, CreatedAt: now.Add(-2 * time.Minute)}
	queue.jobs[kvKey("u1", "f4")] = UploadJob{UserID: "u1", FileID: "f4", Status: JobStatusFailed, Error: "boom", CreatedAt: now.Add(-3 * time.Minute)}

	old := serviceDeps
	serviceDeps = &mockDepsAll{uploadQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp jobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Active) != 2 {
		t.Errorf("active = %d, want 2", len(resp.Active))
	}
	if len(resp.Done) != 1 {
		t.Errorf("done = %d, want 1", len(resp.Done))
	}
	if len(resp.Failed) != 1 {
		t.Errorf("failed = %d, want 1", len(resp.Failed))
	}
}

func TestJobList_EmptyUser(t *testing.T) {
	queue := newStubUploadQueue()

	old := serviceDeps
	serviceDeps = &mockDepsAll{uploadQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "nobody")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp jobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Active) != 0 || len(resp.Failed) != 0 || len(resp.Done) != 0 {
		t.Errorf("expected all empty arrays, got active=%d failed=%d done=%d",
			len(resp.Active), len(resp.Failed), len(resp.Done))
	}
}

func TestJobList_SortedDescending(t *testing.T) {
	queue := newStubUploadQueue()
	now := time.Now()

	queue.jobs[kvKey("u1", "old")] = UploadJob{UserID: "u1", FileID: "old", Status: JobStatusQueued, CreatedAt: now.Add(-10 * time.Minute)}
	queue.jobs[kvKey("u1", "new")] = UploadJob{UserID: "u1", FileID: "new", Status: JobStatusQueued, CreatedAt: now}

	old := serviceDeps
	serviceDeps = &mockDepsAll{uploadQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := clerkCtx(httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody), "u1")
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	var resp jobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil { t.Fatal(err) }

	if len(resp.Active) != 2 {
		t.Fatalf("active = %d, want 2", len(resp.Active))
	}
	if resp.Active[0].FileID != "new" {
		t.Errorf("first active = %q, want 'new' (newest first)", resp.Active[0].FileID)
	}
}
