package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
)

func init() {
	SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHandle_Health(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /health: got status %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("GET /health: Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
}

func TestHandle_OptionsCORS(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS: got status %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("OPTIONS: missing Access-Control-Allow-Origin header")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("OPTIONS: missing Access-Control-Allow-Headers header")
	}
}

func TestHandle_Options_NotProtectedByAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/setup", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS /setup: got status %d, want 204 (middleware must not run for OPTIONS)", rec.Code)
	}
}

func TestHandle_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route: got status %d, want 404", rec.Code)
	}
}

func TestHandle_GetStudents_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	Handle(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /students no auth: got status %d, want 401", rec.Code)
	}
}

func TestHandle_GetStudents_GoogleTokenFailure(t *testing.T) {
	origDeps := serviceDeps
	serviceDeps = &mockDepsGoogleFail{}
	defer func() { serviceDeps = origDeps }()

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	ctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "test-user"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handleGetStudents(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("GET /students token fail: got status %d, want 502", rec.Code)
	}
}

// mockDepsGoogleFail returns error from GoogleServices (simulates OAuth token retrieval failure).
type mockDepsGoogleFail struct{}

func (mockDepsGoogleFail) GoogleServices(r *http.Request) (*googleServices, error) {
	return nil, &apiError{Status: http.StatusBadGateway, Err: nil, Code: "token_failed", Message: "no Google OAuth token found"}
}

func (mockDepsGoogleFail) GetTranscriber() (Transcriber, error) {
	return nil, fmt.Errorf("not configured")
}

func (mockDepsGoogleFail) GetRoster(_ context.Context, _ *googleServices) (Roster, error) {
	return nil, fmt.Errorf("not configured")
}

func (mockDepsGoogleFail) GetDriveStore(_ *googleServices) DriveStore {
	return nil
}

func (mockDepsGoogleFail) GetExtractor() (Extractor, error) {
	return nil, fmt.Errorf("not available")
}

func (mockDepsGoogleFail) GetNoteCreator(_ *googleServices) NoteCreator {
	return nil
}

func (mockDepsGoogleFail) GetMetadataIndex(_ *googleServices) MetadataIndex {
	return nil
}

func (mockDepsGoogleFail) GetExampleStore(_ *googleServices) ExampleStore {
	return nil
}

func (mockDepsGoogleFail) GetExampleExtractor() (ExampleExtractor, error) {
	return nil, fmt.Errorf("not configured")
}

func (mockDepsGoogleFail) GetReportGenerator(_ *googleServices) (ReportGenerator, error) {
	return nil, fmt.Errorf("not configured")
}

func (mockDepsGoogleFail) GoogleServicesForUser(_ context.Context, _ string) (*googleServices, error) {
	return nil, fmt.Errorf("google oauth failed")
}

func (mockDepsGoogleFail) GetUploadQueue() (UploadQueue, error) {
	return nil, fmt.Errorf("not configured")
}

func (mockDepsGoogleFail) GetGradeBeeMetadata(_ context.Context, _ string) (*gradeBeeMetadata, error) {
	return nil, fmt.Errorf("not configured")
}
