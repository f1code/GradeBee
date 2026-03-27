package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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
	methods := rec.Header().Get("Access-Control-Allow-Methods")
	if methods == "" {
		t.Error("OPTIONS: missing Access-Control-Allow-Methods header")
	}
}

func TestHandle_Options_NotProtectedByAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/classes", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS /classes: got status %d, want 204 (middleware must not run for OPTIONS)", rec.Code)
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
