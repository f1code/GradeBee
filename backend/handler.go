package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
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
		handleSetup(rec, r)
	case path == "students" && r.Method == http.MethodGet:
		handleGetStudents(rec, r)
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
