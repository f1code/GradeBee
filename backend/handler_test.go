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
	req := httptest.NewRequest(http.MethodOptions, "/api/classes", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, "OPTIONS: unexpected status")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Origin"), "OPTIONS: missing Access-Control-Allow-Origin header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"), "OPTIONS: missing Access-Control-Allow-Headers header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"), "OPTIONS: missing Access-Control-Allow-Methods header")
}

func TestHandle_Options_NotProtectedByAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/api/classes", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, "OPTIONS /api/classes: middleware must not run for OPTIONS")
}

// TestHandle_UnknownAPIRoute asserts that unknown /api/* paths return 404 JSON.
func TestHandle_UnknownAPIRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "unknown /api route: unexpected status")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// TestHandle_NonAPIRoute_ServesSPA asserts that any non-/api path serves the
// embedded SPA's index.html (placeholder during local builds).
func TestHandle_NonAPIRoute_ServesSPA(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "SPA fallback: unexpected status")
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html", "SPA fallback should be HTML")
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
}

// TestHandle_Root_ServesSPA asserts that GET / serves the SPA, not the JSON health.
func TestHandle_Root_ServesSPA(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
}

func TestHandle_ProtectedEndpoint_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/classes", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "GET /api/classes no auth: unexpected status")
}

// TestHandle_LevelsRoutes asserts each /api/levels route reaches auth
// middleware (401, not 404) — proving the path-matching in Handle() routes
// GET/POST/PUT/DELETE correctly instead of falling through to the unknown
// route branch.
func TestHandle_LevelsRoutes(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/levels"},
		{http.MethodPost, "/api/levels"},
		{http.MethodPut, "/api/levels/1"},
		{http.MethodDelete, "/api/levels/1"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, http.NoBody)
		req.Header.Set("Authorization", "Bearer invalid-token")
		rec := httptest.NewRecorder()

		Handle(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s: expected routed-then-rejected, got %d", c.method, c.path, rec.Code)
	}
}
