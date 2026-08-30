// handler.go is the main HTTP entrypoint for the GradeBee backend. It wires
// together routing, CORS headers, request-scoped logging, and response timing.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
)

func init() {
	// Ensure the Clerk SDK has the secret key for JWT verification.
	if key := os.Getenv("CLERK_SECRET_KEY"); key != "" {
		clerk.SetKey(key)
	}
}

// clerkAuthMiddleware wraps a handler with Clerk JWT verification and the
// active-Organisation check. Failures log the token's kid/issuer/expiry only —
// never the subject, the header, or anything about the secret key.
func clerkAuthMiddleware(next http.Handler) http.Handler {
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
		var expiry string
		if decoded.Expiry != nil {
			expiry = time.Unix(*decoded.Expiry, 0).UTC().Format(time.RFC3339)
		}
		log.Debug("auth: jwt decoded", "kid", decoded.KeyID, "expires", expiry)

		// Tag the Sentry scope with the authenticated user so events can be
		// correlated to a specific user in the Sentry dashboard.
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.Scope().SetUser(sentry.User{ID: decoded.Subject})
		}

		verified := false
		inner := clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			verified = true
			// Enforce active Clerk Organisation — every API request must belong
			// to a Group. Personal (org-less) sessions are not permitted.
			claims, ok := clerk.SessionClaimsFromContext(r.Context())
			if !ok || claims == nil || claims.ActiveOrganizationID == "" {
				writeAPIError(w, r, &apiError{
					Status:  http.StatusForbidden,
					Code:    "no_active_org",
					Message: "no active organization \u2014 ask your admin for an invitation",
				})
				return
			}
			next.ServeHTTP(w, r)
		}))
		inner.ServeHTTP(w, r)

		if !verified {
			log.Warn("auth: clerk verification failed",
				"kid", decoded.KeyID,
				"issuer", decoded.Issuer,
				"token_expired", decoded.Expiry != nil && time.Unix(*decoded.Expiry, 0).Before(time.Now()),
			)
		}
	})
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
		return "", &apiError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "missing or invalid session"}
	}
	return claims.Subject, nil
}

// Handle is the main HTTP entrypoint, used by cmd/server/main.go. It owns the
// request-scoped logger, the health probe, the SPA fallthrough and CORS; every
// /api/ request is then dispatched by apiMux (see router.go).
func Handle(w http.ResponseWriter, r *http.Request) {
	reqID := uuid.New().String()
	reqLogger := getLogger().With("request_id", reqID)
	r = r.WithContext(context.WithValue(r.Context(), loggerKey, reqLogger))

	w.Header().Set("X-Request-ID", reqID)

	rawPath := strings.TrimPrefix(r.URL.Path, "/")

	// Health probe lives at /health (outside /api/) for simplicity (Dokku/uptime checks).
	if rawPath == "health" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Anything not under /api/ is treated as a static/SPA request.
	if !strings.HasPrefix(rawPath, "api/") && rawPath != "api" {
		spaHandler().ServeHTTP(w, r)
		return
	}

	// API routes — set JSON content-type and CORS headers.
	w.Header().Set("Content-Type", "application/json")

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

	rec := &statusRecorder{ResponseWriter: w, status: 0}
	start := time.Now()

	apiMux.ServeHTTP(rec, r)

	duration := time.Since(start).Milliseconds()
	logAttrs := []any{"method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", duration}
	switch {
	case rec.status == 401 || rec.status == 403:
		logAttrs = append(logAttrs,
			"has_auth_header", r.Header.Get("Authorization") != "",
			"auth_header_prefix", safePrefix(r.Header.Get("Authorization"), 20),
		)
		reqLogger.Warn("request completed (auth failure)", logAttrs...)
	case rec.status >= 400:
		reqLogger.Warn("request completed", logAttrs...)
	default:
		reqLogger.Info("request completed", logAttrs...)
	}
}

// safePrefix returns the first n bytes of s, or s if shorter.
func safePrefix(s string, n int) string {
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

// callerName returns the unqualified name of the handler that called
// requireStudentOwnership, e.g. "handleGetNote". It labels a log record with its
// call site without putting any caller-controlled text into telemetry.
//
// The skip of 2 (callerName -> requireStudentOwnership -> the handler) assumes this
// is called directly from requireStudentOwnership. Introducing a wrapper between the
// two would silently retarget the label to the wrapper's caller rather than fail to
// compile. TestHandleGenerateReports_OwnershipArms pins the exact value
// "op":"handleGenerateReports" through a real handler and fails if the depth drifts.
func callerName() string {
	return callerAt(2)
}

// callerAt returns the unqualified function name skip frames above the function
// that called callerAt: callerAt(0) names that function itself, callerAt(1) its
// caller. writeInternalError and writeError use it to label a 500 with the
// handler that gave up.
func callerAt(skip int) string {
	pc, _, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}
	name := runtime.FuncForPC(pc).Name()
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// requireStudentOwnership gates a handler on the caller owning the student, writing
// the 404 itself when they do not. It returns true only when ownership is confirmed,
// so call sites read `if !requireStudentOwnership(...) { return }`.
//
// It exists because the two ways this check fails are different events that must stay
// distinguishable in telemetry while staying indistinguishable to the caller:
//
//   - the check could not run — an outage, logged at Error. A repo failure must not
//     vanish behind a 404.
//   - the check ran and said no — a denial, logged at Warn. BelongsToUser cannot tell
//     "no such student" from "another teacher's student", so this one record covers a
//     deletion race against a stale roster, a client bug, and probing alike. Warn
//     keeps it queryable without paging anyone; the false-positive rate from the
//     deletion race is still unknown.
//
// Both arms write notFoundMsg with the same status, so the response is byte-identical
// either way and tells the caller nothing about which occurred.
//
// notFoundMsg is caller-facing text and may legitimately carry a student name (see
// handleGenerateReports, which echoes the name the caller itself supplied). It is
// therefore never logged: telemetry carries student_id only, per ADR 0003.
//
// The record names its handler via callerName() rather than r.URL.Path. The request
// logger carries only request_id, so without some site label all sixteen call sites
// emit the same undifferentiated line — but the live path is client-controlled text.
// The router only matches exact patterns now, so a stray trailing segment is a 404
// before any handler runs; still, the {id} wildcard itself is caller-supplied, and
// a handler may be invoked directly (tests, or a future route) with any path at all.
// The handler name cannot carry caller input, whatever the path holds.
func requireStudentOwnership(w http.ResponseWriter, r *http.Request, studentID int64, userID, notFoundMsg string) bool {
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), studentID, userID)
	log := loggerFromRequest(r)
	switch {
	case err != nil:
		log.Error("ownership check failed", "student_id", studentID,
			"method", r.Method, "op", callerName(), "error", err)
	case !owns:
		log.Warn("ownership check denied", "student_id", studentID,
			"method", r.Method, "op", callerName())
	default:
		return true
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": notFoundMsg})
	return false
}
