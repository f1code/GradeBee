package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// routerNotFoundBody is the byte-exact body the "/api/" catch-all writes. Handlers
// that miss a row write the same text through writeError, so the tests below
// tell the two apart with a header the auth wrapper stamps on every routed request.
const routerNotFoundBody = "{\"error\":\"not found\"}\n"

// routedHeader marks a response whose request reached a registered route.
const routedHeader = "X-Test-Routed"

// routerTestMux builds an API mux whose auth wrapper stamps routedHeader before
// injecting claims, on top of deps that let every handler run without panicking.
func routerTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	db := setupTestDB(t)
	withDeps(t, &mockDepsAll{
		db:             db,
		classRepo:      &ClassRepo{db: db},
		studentRepo:    &StudentRepo{db: db},
		noteRepo:       &NoteRepo{db: db},
		reportRepo:     &ReportRepo{db: db},
		voiceNoteRepo:  &VoiceNoteRepo{db: db},
		feedbackRepo:   &ArtifactFeedbackRepo{db: db},
		levelRepo:      &LevelRepo{db: db},
		voiceNoteQueue: newStubVoiceNoteQueue(),
		driveClientErr: errors.New("no drive in tests"),
		uploadsDir:     t.TempDir(),
	})
	// Keep handleGoogleToken off the network whatever the developer's shell holds.
	t.Setenv("CLERK_SECRET_KEY", "")
	mark := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(routedHeader, "1")
			fakeAuth("user_test", "org_test", "org:admin")(next).ServeHTTP(w, r)
		})
	}
	return newAPIMux(mark)
}

// wildcardSegment matches one {name} path wildcard in a ServeMux pattern.
var wildcardSegment = regexp.MustCompile(`\{[^}]+\}`)

// samplePath turns a route pattern into a concrete path by replacing every
// wildcard with "1", so a table entry can be driven without hand-written ids.
func samplePath(pattern string) string {
	return wildcardSegment.ReplaceAllString(pattern, "1")
}

// TestAPIMux_Routes drives every apiRoutes entry through newAPIMux and asserts
// each reaches its handler rather than the catch-all. Iterating the real table
// rather than a copy means a new route is covered the moment it is registered.
func TestAPIMux_Routes(t *testing.T) {
	mux := routerTestMux(t)
	require.NotEmpty(t, apiRoutes)

	for _, rt := range apiRoutes {
		path := samplePath(rt.Pattern)
		t.Run(rt.Method+" "+path, func(t *testing.T) {
			req := httptest.NewRequest(rt.Method, path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, "1", rec.Header().Get(routedHeader), "did not reach a registered route: %d %s", rec.Code, rec.Body.String())
			assert.NotEqual(t, http.StatusMethodNotAllowed, rec.Code)
		})
	}
}

// TestAPIMux_NotFound pins the router's own 404: unknown paths, a known path with
// the wrong method, and a matched pattern with a trailing segment all answer the
// same JSON body and never reach a handler (no routedHeader, never a 405).
func TestAPIMux_NotFound(t *testing.T) {
	mux := routerTestMux(t)

	cases := []struct{ name, method, path string }{
		{"unknown path", http.MethodGet, "/api/nonexistent"},
		{"wrong method on known path", http.MethodPatch, "/api/classes"},
		{"wrong method on wildcard path", http.MethodPost, "/api/notes/5"},
		{"trailing segment after wildcard", http.MethodGet, "/api/notes/5/extra"},
		{"bare /api", http.MethodGet, "/api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, http.NoBody)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNotFound, rec.Code)
			assert.Equal(t, routerNotFoundBody, rec.Body.String())
			assert.Empty(t, rec.Header().Get(routedHeader), "must not reach a handler")
		})
	}
}

// TestHandle_MethodMismatch_Is404 asserts the public entrypoint keeps the
// pre-ServeMux contract: a wrong method on a known path is a JSON 404, not 405.
func TestHandle_MethodMismatch_Is404(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/api/classes", http.NoBody)
	rec := httptest.NewRecorder()

	Handle(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, routerNotFoundBody, rec.Body.String())
}
