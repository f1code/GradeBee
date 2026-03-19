// handler.go is the main HTTP entrypoint for the GradeBee backend. It wires
// together routing, CORS headers, request-scoped logging, and response timing.
// The exported Handle function is invoked by the Scaleway serverless runtime
// (and by the local dev server in cmd/server/main.go).
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/google/uuid"
)

var (
	setupHandler      = clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(handleSetup))
	studentsHandler   = clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(handleGetStudents))
	uploadHandler     = clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(handleUpload))
	transcribeHandler = clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(handleTranscribe))
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

// Handle is the Scaleway serverless function entrypoint.
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
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	rec := &statusRecorder{ResponseWriter: w, status: 0}
	start := time.Now()

	switch {
	case (path == "" || path == "health") && r.Method == http.MethodGet:
		writeJSON(rec, http.StatusOK, map[string]string{"status": "ok"})
	case path == "setup" && r.Method == http.MethodPost:
		setupHandler.ServeHTTP(rec, r)
	case path == "students" && r.Method == http.MethodGet:
		studentsHandler.ServeHTTP(rec, r)
	case path == "upload" && r.Method == http.MethodPost:
		uploadHandler.ServeHTTP(rec, r)
	case path == "transcribe" && r.Method == http.MethodPost:
		transcribeHandler.ServeHTTP(rec, r)
	default:
		writeJSON(rec, http.StatusNotFound, map[string]string{"error": "not found"})
	}

	duration := time.Since(start).Milliseconds()
	logAttrs := []any{"method", r.Method, "path", "/"+path, "status", rec.status, "duration_ms", duration}
	if rec.status >= 400 {
		reqLogger.Warn("request completed", logAttrs...)
	} else if path != "" && path != "health" {
		reqLogger.Info("request completed", logAttrs...)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		getLogger().Error("json encode error", "error", err)
	}
}
