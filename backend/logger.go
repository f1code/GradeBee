// logger.go initialises and exposes a package-level structured logger built on
// top of the standard library's log/slog. Log level and format (text or JSON)
// are controlled via LOG_LEVEL and LOG_FORMAT environment variables.
//
// When SENTRY_DSN is set, InitLogger wires a sentryslog handler alongside the
// stdout handler so that all structured log records are forwarded to Sentry
// Logs. Error/Fatal records are also captured as Sentry events (Issues) by
// default sentryslog behaviour.
//
// A request-scoped logger can be attached to a context and retrieved via
// loggerFromContext / loggerFromRequest.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"

	sentryslog "github.com/getsentry/sentry-go/slog"
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
	// Bootstrap a stdout-only logger so logging works before InitLogger is
	// called (e.g. during --migrate-only or in tests).
	logMu.Lock()
	log = slog.New(buildStdoutHandler())
	logMu.Unlock()
	slog.SetDefault(log)
}

// InitLogger wires the package logger. It must be called after InitSentry so
// that the sentryslog handler can attach to the already-configured Sentry
// client. When SENTRY_DSN is unset the logger falls back to stdout only.
func InitLogger() {
	stdoutH := buildStdoutHandler()

	var h slog.Handler
	if os.Getenv("SENTRY_DSN") != "" {
		// sentryslog defaults: Error/Fatal records are sent as Sentry events
		// (Issues) in addition to structured log entries; Debug/Info/Warn are
		// sent as structured log entries only.
		sentryH := sentryslog.Option{}.NewSentryHandler(context.Background())
		h = slog.NewMultiHandler(stdoutH, sentryH)
	} else {
		h = stdoutH
	}

	logMu.Lock()
	log = slog.New(h)
	logMu.Unlock()
	slog.SetDefault(log)
}

// buildStdoutHandler constructs the stdout slog handler from environment vars.
func buildStdoutHandler() slog.Handler {
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

	if os.Getenv("LOG_FORMAT") == "json" {
		return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	return slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
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
