// sentry.go initialises the Sentry SDK and exposes helpers for capturing
// non-error feedback events from server-side code paths.
package handler

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/getsentry/sentry-go"
)

// sentryInitialised tracks whether InitSentry successfully configured a client.
// Used by CaptureFeedback to short-circuit when Sentry is not active.
var sentryInitialised bool

// InitSentry reads SENTRY_DSN and SENTRY_RELEASE from the environment and
// initialises the Sentry SDK. It is a no-op (and does not fail) if SENTRY_DSN
// is empty, so local dev works without any configuration.
//
// Call this once at program startup (before serving requests). The caller is
// responsible for calling sentry.Flush before process exit.
func InitSentry() {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		slog.Info("sentry: SENTRY_DSN not set, skipping initialisation")
		return
	}

	release := os.Getenv("SENTRY_RELEASE")
	if release == "" {
		release = "dev"
	}

	environment := os.Getenv("SENTRY_ENVIRONMENT")

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Release:          release,
		Environment:      environment,
		EnableLogs:       true, // route structured logs to Sentry Logs
		TracesSampleRate: 0.0,  // performance tracing disabled
		BeforeSend:       scrubBeforeSend,
	})
	if err != nil {
		slog.Error("sentry: initialisation failed", "error", err)
		return
	}

	sentryInitialised = true
	slog.Info("sentry: initialised", "release", release, "environment", environment)
}

// feedbackEvent holds the fields for an explicit user-feedback event (e.g.
// a thumbs-down on a generated report card). All fields are optional.
type feedbackEvent struct {
	UserID    string
	StudentID int64
	ReportID  int64
	Rating    string // e.g. "thumbs_down"
	Comment   string
}

// captureFeedback sends a non-error feedback event to Sentry. It is a no-op
// if Sentry was not initialised (i.e. SENTRY_DSN is unset in this environment).
func captureFeedback(ctx context.Context, ev feedbackEvent) {
	if !sentryInitialised {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		if ev.UserID != "" {
			scope.SetUser(sentry.User{ID: ev.UserID})
		}
		scope.SetTag("rating", ev.Rating)
		scope.SetTag("feedback", "true")
		scope.SetLevel(sentry.LevelInfo)
		scope.SetContext("feedback", map[string]any{
			"student_id": ev.StudentID,
			"report_id":  ev.ReportID,
			"comment":    ev.Comment,
		})
		hub.CaptureMessage("user_feedback")
	})
}

// namePattern matches two or more capitalised words separated by a space —
// a heuristic for student names (e.g. "Alice Smith", "Jean-Luc Picard").
var namePattern = regexp.MustCompile(`\b[A-Z][a-z]+(?:[-\s][A-Z][a-z]+)+\b`)

// scrubBeforeSend is the Sentry BeforeSend hook. It removes the raw request
// body and query string, and redacts name-shaped strings from exception values
// to reduce the risk of sending student PII.
func scrubBeforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	// Scrub HTTP request details.
	if event.Request != nil {
		event.Request.Data = ""
		event.Request.QueryString = ""
		event.Request.Cookies = ""
		// Keep method, URL path, and headers (minus Cookie).
		delete(event.Request.Headers, "Cookie")
		delete(event.Request.Headers, "Authorization")
	}

	// Redact name-shaped strings from exception values and stack-frame vars.
	for i := range event.Exception {
		event.Exception[i].Value = redactNames(event.Exception[i].Value)
		if event.Exception[i].Stacktrace != nil {
			for j := range event.Exception[i].Stacktrace.Frames {
				f := &event.Exception[i].Stacktrace.Frames[j]
				for k, v := range f.Vars {
					if s, ok := v.(string); ok {
						f.Vars[k] = redactNames(s)
					}
				}
			}
		}
	}

	return event
}

// redactNames replaces name-shaped substrings with "[REDACTED]".
func redactNames(s string) string {
	return namePattern.ReplaceAllStringFunc(s, func(match string) string {
		// Keep single-word capitalised tokens (e.g. class names like "English").
		if !strings.Contains(match, " ") && !strings.Contains(match, "-") {
			return match
		}
		return "[REDACTED]"
	})
}
