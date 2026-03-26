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
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/google/uuid"
)

func init() {
	// Ensure the Clerk SDK has the secret key for JWT verification.
	// Also called in cmd/server/main.go, but init() runs first.
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

		// Try decoding (no verification yet — just parse claims)
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

		// Now delegate to Clerk's full middleware for actual verification
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
			// Try fetching the JWK directly for more diagnostics
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

var (
	getSetupHandler   = debugAuthMiddleware(http.HandlerFunc(handleGetSetup))
	setupHandler      = debugAuthMiddleware(http.HandlerFunc(handleSetup))
	studentsHandler   = debugAuthMiddleware(http.HandlerFunc(handleGetStudents))
	uploadHandler     = debugAuthMiddleware(http.HandlerFunc(handleUpload))
	transcribeHandler = debugAuthMiddleware(http.HandlerFunc(handleTranscribe))
	extractHandler    = debugAuthMiddleware(http.HandlerFunc(handleExtract))
	notesHandler      = debugAuthMiddleware(http.HandlerFunc(handleCreateNotes))
	reportExamplesListHandler   = debugAuthMiddleware(http.HandlerFunc(handleListReportExamples))
	reportExamplesUploadHandler = debugAuthMiddleware(http.HandlerFunc(handleUploadReportExample))
	reportExamplesDeleteHandler = debugAuthMiddleware(http.HandlerFunc(handleDeleteReportExample))
	reportsHandler              = debugAuthMiddleware(http.HandlerFunc(handleGenerateReports))
	reportsRegenerateHandler    = debugAuthMiddleware(http.HandlerFunc(handleRegenerateReport))
	googleTokenHandler          = debugAuthMiddleware(http.HandlerFunc(handleGoogleToken))
	driveImportHandler          = debugAuthMiddleware(http.HandlerFunc(handleDriveImport))
	jobListHandler              = debugAuthMiddleware(http.HandlerFunc(handleJobList))
	jobRetryHandler             = debugAuthMiddleware(http.HandlerFunc(handleJobRetry))
)

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
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	rec := &statusRecorder{ResponseWriter: w, status: 0}
	start := time.Now()

	// Debug auth: log presence/shape of Authorization header for authenticated routes
	if path != "" && path != "health" {
		authHeader := r.Header.Get("Authorization")
		reqLogger.Debug("incoming request",
			"path", path,
			"has_auth_header", authHeader != "",
			"auth_header_len", len(authHeader),
			"auth_header_prefix", truncate(authHeader, 20),
		)
	}

	switch {
	case (path == "" || path == "health") && r.Method == http.MethodGet:
		writeJSON(rec, http.StatusOK, map[string]string{"status": "ok"})
	case path == "setup" && r.Method == http.MethodGet:
		getSetupHandler.ServeHTTP(rec, r)
	case path == "setup" && r.Method == http.MethodPost:
		setupHandler.ServeHTTP(rec, r)
	case path == "students" && r.Method == http.MethodGet:
		studentsHandler.ServeHTTP(rec, r)
	case path == "upload" && r.Method == http.MethodPost:
		uploadHandler.ServeHTTP(rec, r)
	case path == "transcribe" && r.Method == http.MethodPost:
		transcribeHandler.ServeHTTP(rec, r)
	case path == "extract" && r.Method == http.MethodPost:
		extractHandler.ServeHTTP(rec, r)
	case path == "notes" && r.Method == http.MethodPost:
		notesHandler.ServeHTTP(rec, r)
	case path == "report-examples" && r.Method == http.MethodGet:
		reportExamplesListHandler.ServeHTTP(rec, r)
	case path == "report-examples" && r.Method == http.MethodPost:
		reportExamplesUploadHandler.ServeHTTP(rec, r)
	case path == "report-examples" && r.Method == http.MethodDelete:
		reportExamplesDeleteHandler.ServeHTTP(rec, r)
	case path == "reports" && r.Method == http.MethodPost:
		reportsHandler.ServeHTTP(rec, r)
	case path == "reports/regenerate" && r.Method == http.MethodPost:
		reportsRegenerateHandler.ServeHTTP(rec, r)
	case path == "google-token" && r.Method == http.MethodGet:
		googleTokenHandler.ServeHTTP(rec, r)
	case path == "drive-import" && r.Method == http.MethodPost:
		driveImportHandler.ServeHTTP(rec, r)
	case path == "jobs" && r.Method == http.MethodGet:
		jobListHandler.ServeHTTP(rec, r)
	case path == "jobs/retry" && r.Method == http.MethodPost:
		jobRetryHandler.ServeHTTP(rec, r)
	default:
		writeJSON(rec, http.StatusNotFound, map[string]string{"error": "not found"})
	}

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
