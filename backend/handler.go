// handler.go is the main HTTP entrypoint for the GradeBee backend. It wires
// together routing, CORS headers, request-scoped logging, and response timing.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/google/uuid"
)

func init() {
	// Ensure the Clerk SDK has the secret key for JWT verification.
	key := os.Getenv("CLERK_SECRET_KEY")
	slog.Info("clerk init",
		"key_set", key != "",
		"key_len", len(key),
		"key_prefix", safePrefix(key, 12),
	)
	if key != "" {
		clerk.SetKey(key)
	}
}

// safePrefix returns the first n bytes of s, or s if shorter.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// debugAuthMiddleware wraps a handler with Clerk JWT verification and logs
// detailed information when authentication fails.
func debugAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := loggerFromRequest(r)

		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" || token == authHeader {
			log.Warn("auth: no Bearer token found")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		decoded, err := jwt.Decode(r.Context(), &jwt.DecodeParams{Token: token})
		if err != nil {
			log.Warn("auth: jwt decode failed", "error", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var expiry, issuedAt string
		if decoded.Expiry != nil {
			expiry = time.Unix(*decoded.Expiry, 0).UTC().Format(time.RFC3339)
		}
		if decoded.IssuedAt != nil {
			issuedAt = time.Unix(*decoded.IssuedAt, 0).UTC().Format(time.RFC3339)
		}
		log.Info("auth: jwt decoded",
			"kid", decoded.KeyID,
			"issuer", decoded.Issuer,
			"subject", decoded.Subject,
			"expires", expiry,
			"issued_at", issuedAt,
		)

		verified := false
		inner := clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			verified = true
			next.ServeHTTP(w, r)
		}))
		inner.ServeHTTP(w, r)

		if !verified {
			secretKey := os.Getenv("CLERK_SECRET_KEY")
			log.Warn("auth: clerk verification failed",
				"kid", decoded.KeyID,
				"issuer", decoded.Issuer,
				"clerk_secret_key_set", secretKey != "",
				"clerk_secret_key_len", len(secretKey),
				"clerk_secret_key_prefix", truncate(secretKey, 12),
				"now_utc", time.Now().UTC().Format(time.RFC3339),
				"token_expired", decoded.Expiry != nil && time.Unix(*decoded.Expiry, 0).Before(time.Now()),
			)
			if decoded.KeyID != "" {
				_, jwkErr := jwt.GetJSONWebKey(r.Context(), &jwt.GetJSONWebKeyParams{KeyID: decoded.KeyID})
				if jwkErr != nil {
					log.Warn("auth: jwk fetch failed", "kid", decoded.KeyID, "error", fmt.Sprintf("%v", jwkErr))
				} else {
					log.Info("auth: jwk fetch succeeded", "kid", decoded.KeyID)
				}
			}
		}
	})
}

// authHandler wraps a handler function with auth middleware.
func authHandler(fn http.HandlerFunc) http.Handler {
	return debugAuthMiddleware(http.HandlerFunc(fn))
}

// statusRecorder wraps ResponseWriter to capture status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// userIDFromRequest extracts the Clerk user ID from JWT session claims.
func userIDFromRequest(r *http.Request) (string, error) {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return "", &apiError{Status: http.StatusForbidden, Code: "unauthorized", Message: "missing or invalid session"}
	}
	return claims.Subject, nil
}

// pathParam extracts a numeric ID from a URL path segment.
// e.g. pathParam("classes/42/students", "classes/", "/students") returns 42.
func pathParam(path, prefix string) (int64, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path {
		return 0, false
	}
	// rest is "42/students" or "42"
	idx := strings.Index(rest, "/")
	idStr := rest
	if idx >= 0 {
		idStr = rest[:idx]
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// Handle is the main HTTP entrypoint, used by cmd/server/main.go.
func Handle(w http.ResponseWriter, r *http.Request) {
	reqID := uuid.New().String()
	reqLogger := getLogger().With("request_id", reqID)
	r = r.WithContext(context.WithValue(r.Context(), loggerKey, reqLogger))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", reqID)

	// CORS
	origin := os.Getenv("ALLOWED_ORIGIN")
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	rec := &statusRecorder{ResponseWriter: w, status: 0}
	start := time.Now()

	if path != "" && path != "health" {
		authHeader := r.Header.Get("Authorization")
		reqLogger.Debug("incoming request",
			"path", path,
			"has_auth_header", authHeader != "",
			"auth_header_len", len(authHeader),
			"auth_header_prefix", truncate(authHeader, 20),
		)
	}

	// Route matching
	matched := true
	switch {
	// Health
	case (path == "" || path == "health") && r.Method == http.MethodGet:
		writeJSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	// Classes CRUD
	case path == "classes" && r.Method == http.MethodGet:
		authHandler(handleListClasses).ServeHTTP(rec, r)
	case path == "classes" && r.Method == http.MethodPost:
		authHandler(handleCreateClass).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "classes/") && !strings.Contains(strings.TrimPrefix(path, "classes/"), "/") && r.Method == http.MethodPut:
		authHandler(handleUpdateClass).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "classes/") && !strings.Contains(strings.TrimPrefix(path, "classes/"), "/") && r.Method == http.MethodDelete:
		authHandler(handleDeleteClass).ServeHTTP(rec, r)

	// Students under class
	case strings.HasPrefix(path, "classes/") && strings.HasSuffix(path, "/students") && r.Method == http.MethodGet:
		authHandler(handleListStudents).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "classes/") && strings.HasSuffix(path, "/students") && r.Method == http.MethodPost:
		authHandler(handleCreateStudent).ServeHTTP(rec, r)

	// Students by ID
	case path == "students" && r.Method == http.MethodGet:
		authHandler(handleGetStudents).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "students/") && !strings.Contains(strings.TrimPrefix(path, "students/"), "/") && r.Method == http.MethodPut:
		authHandler(handleUpdateStudent).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "students/") && !strings.Contains(strings.TrimPrefix(path, "students/"), "/") && r.Method == http.MethodDelete:
		authHandler(handleDeleteStudent).ServeHTTP(rec, r)

	// Notes under student
	case strings.HasPrefix(path, "students/") && strings.HasSuffix(path, "/notes") && r.Method == http.MethodGet:
		authHandler(handleListNotes).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "students/") && strings.HasSuffix(path, "/notes") && r.Method == http.MethodPost:
		authHandler(handleCreateNote).ServeHTTP(rec, r)

	// Notes by ID
	case strings.HasPrefix(path, "notes/") && r.Method == http.MethodGet:
		authHandler(handleGetNote).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "notes/") && r.Method == http.MethodPut:
		authHandler(handleUpdateNote).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "notes/") && r.Method == http.MethodDelete:
		authHandler(handleDeleteNote).ServeHTTP(rec, r)

	// Reports
	case path == "reports" && r.Method == http.MethodPost:
		authHandler(handleGenerateReports).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "reports/") && strings.HasSuffix(path, "/regenerate") && r.Method == http.MethodPost:
		authHandler(handleRegenerateReport).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "students/") && strings.HasSuffix(path, "/reports") && r.Method == http.MethodGet:
		authHandler(handleListReports).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "reports/") && r.Method == http.MethodGet:
		authHandler(handleGetReport).ServeHTTP(rec, r)
	case strings.HasPrefix(path, "reports/") && r.Method == http.MethodDelete:
		authHandler(handleDeleteReport).ServeHTTP(rec, r)

	// Report examples
	case path == "report-examples" && r.Method == http.MethodGet:
		authHandler(handleListReportExamples).ServeHTTP(rec, r)
	case path == "report-examples" && r.Method == http.MethodPost:
		authHandler(handleUploadReportExample).ServeHTTP(rec, r)
	case path == "report-examples" && r.Method == http.MethodDelete:
		authHandler(handleDeleteReportExample).ServeHTTP(rec, r)

	// Upload + Drive import
	case path == "upload" && r.Method == http.MethodPost:
		authHandler(handleUpload).ServeHTTP(rec, r)
	case path == "drive-import" && r.Method == http.MethodPost:
		authHandler(handleDriveImport).ServeHTTP(rec, r)

	// Google token (for Drive Picker)
	case path == "google-token" && r.Method == http.MethodGet:
		authHandler(handleGoogleToken).ServeHTTP(rec, r)

	// Jobs
	case path == "jobs" && r.Method == http.MethodGet:
		authHandler(handleJobList).ServeHTTP(rec, r)
	case path == "jobs/retry" && r.Method == http.MethodPost:
		authHandler(handleJobRetry).ServeHTTP(rec, r)
	case path == "jobs/dismiss" && r.Method == http.MethodPost:
		authHandler(handleJobDismiss).ServeHTTP(rec, r)

	default:
		matched = false
		writeJSON(rec, http.StatusNotFound, map[string]string{"error": "not found"})
	}
	_ = matched

	duration := time.Since(start).Milliseconds()
	logAttrs := []any{"method", r.Method, "path", "/" + path, "status", rec.status, "duration_ms", duration}
	switch {
	case rec.status == 401 || rec.status == 403:
		logAttrs = append(logAttrs,
			"has_auth_header", r.Header.Get("Authorization") != "",
			"auth_header_prefix", truncate(r.Header.Get("Authorization"), 20),
		)
		reqLogger.Warn("request completed (auth failure)", logAttrs...)
	case rec.status >= 400:
		reqLogger.Warn("request completed", logAttrs...)
	case path != "" && path != "health":
		reqLogger.Info("request completed", logAttrs...)
	}
}

// truncate returns the first n characters of s, or s if shorter.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		getLogger().Error("json encode error", "error", err)
	}
}
