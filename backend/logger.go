package handler

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
)

type contextKey int

const loggerKey contextKey = iota

// loggerFromRequest returns a request-scoped logger with request_id, or the package logger.
func loggerFromRequest(r *http.Request) *slog.Logger {
	return loggerFromContext(r.Context())
}

// loggerFromContext returns a request-scoped logger from context, or the package logger.
func loggerFromContext(ctx context.Context) *slog.Logger {
	if l := ctx.Value(loggerKey); l != nil {
		if logger, ok := l.(*slog.Logger); ok {
			return logger
		}
	}
	return getLogger()
}

var (
	log   *slog.Logger
	logMu sync.RWMutex
)

func init() {
	level := slog.LevelInfo
	if s := os.Getenv("LOG_LEVEL"); s != "" {
		switch s {
		case "DEBUG":
			level = slog.LevelDebug
		case "INFO":
			level = slog.LevelInfo
		case "WARN":
			level = slog.LevelWarn
		case "ERROR":
			level = slog.LevelError
		case "disabled", "off":
			level = slog.Level(-8) // LevelDisabled in Go 1.22+
		}
	}

	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}

	logMu.Lock()
	log = slog.New(handler)
	logMu.Unlock()
	slog.SetDefault(log)
}

// getLogger returns the package logger. Safe for concurrent use.
func getLogger() *slog.Logger {
	logMu.RLock()
	defer logMu.RUnlock()
	if log != nil {
		return log
	}
	return slog.Default()
}

// SetLogger replaces the package logger. Used by tests to suppress output.
func SetLogger(l *slog.Logger) {
	logMu.Lock()
	defer logMu.Unlock()
	log = l
}
