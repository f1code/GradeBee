package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func init() {
	SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHandle_Health(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "GET /health: unexpected status")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), "GET /health: wrong Content-Type")
}

func TestHandle_OptionsCORS(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, "OPTIONS: unexpected status")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Origin"), "OPTIONS: missing Access-Control-Allow-Origin header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"), "OPTIONS: missing Access-Control-Allow-Headers header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"), "OPTIONS: missing Access-Control-Allow-Methods header")
}

func TestHandle_Options_NotProtectedByAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/classes", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, "OPTIONS /classes: middleware must not run for OPTIONS")
}

func TestHandle_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "unknown route: unexpected status")
}

func TestHandle_GetStudents_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "GET /students no auth: unexpected status")
}
