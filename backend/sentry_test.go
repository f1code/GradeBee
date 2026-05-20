package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitSentry_NoopWhenDSNUnset verifies that calling InitSentry with no
// SENTRY_DSN set leaves the global hub without a client and does not panic.
func TestInitSentry_NoopWhenDSNUnset(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")

	// Reset package state so a previous test's initialisation doesn't bleed in.
	orig := sentryInitialised
	sentryInitialised = false
	t.Cleanup(func() { sentryInitialised = orig })

	assert.NotPanics(t, InitSentry)
	assert.False(t, sentryInitialised, "sentryInitialised should remain false when DSN is unset")
	assert.Nil(t, sentry.CurrentHub().Client(), "Sentry client should be nil when DSN is unset")
}

// TestCaptureFeedback_NoopWhenUninitialized ensures CaptureFeedback does not
// panic when Sentry is not initialised.
func TestCaptureFeedback_NoopWhenUninitialized(t *testing.T) {
	orig := sentryInitialised
	sentryInitialised = false
	t.Cleanup(func() { sentryInitialised = orig })

	assert.NotPanics(t, func() {
		captureFeedback(context.Background(), feedbackEvent{
			UserID:    "user_abc",
			StudentID: 1,
			ReportID:  2,
			Rating:    "thumbs_down",
			Comment:   "Not accurate",
		})
	})
}

// TestSentryMiddleware_CapturesPanic verifies that a handler panic is captured
// by the sentryhttp middleware and delivered to the Sentry transport.
func TestSentryMiddleware_CapturesPanic(t *testing.T) {
	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@o0.ingest.sentry.io/0",
		Transport: transport,
	})
	require.NoError(t, err)

	hub := sentry.NewHub(client, sentry.NewScope())

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// sentryhttp with Repanic:false so the test doesn't re-panic after capture.
	middleware := sentryhttp.New(sentryhttp.Options{
		Repanic:         false,
		WaitForDelivery: true,
	})
	wrapped := middleware.Handle(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	// Inject the test hub into the request context.
	req = req.WithContext(sentry.SetHubOnContext(req.Context(), hub))
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		wrapped.ServeHTTP(rec, req)
	})

	events := transport.Events()
	require.Len(t, events, 1, "expected exactly one Sentry event from the panic")
	assert.Equal(t, sentry.LevelFatal, events[0].Level)
}

// TestScrubBeforeSend_ScrubbsRequestBody checks that the BeforeSend hook
// removes the request body and query string.
func TestScrubBeforeSend_ScrubbsRequestBody(t *testing.T) {
	event := &sentry.Event{
		Request: &sentry.Request{
			Data:        `{"student":"Alice Smith"}`,
			QueryString: "token=secret",
			Cookies:     "session=abc",
			Headers: map[string]string{
				"Authorization": "Bearer tok",
				"Content-Type":  "application/json",
			},
		},
	}

	result := scrubBeforeSend(event, nil)

	assert.Empty(t, result.Request.Data)
	assert.Empty(t, result.Request.QueryString)
	assert.Empty(t, result.Request.Cookies)
	assert.NotContains(t, result.Request.Headers, "Authorization")
	assert.NotContains(t, result.Request.Headers, "Cookie")
	assert.Equal(t, "application/json", result.Request.Headers["Content-Type"], "non-sensitive headers preserved")
}

// TestRedactNames checks that the name-shaped heuristic redacts student names.
func TestRedactNames(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Error processing Alice Smith report", "Error processing [REDACTED] report"},
		{"Jean-Luc Picard failed", "[REDACTED] failed"},
		{"English class notes", "English class notes"}, // single capitalised word — keep
		{"no names here at all", "no names here at all"},
		{"Multiple: Alice Smith and Bob Jones both failed", "Multiple: [REDACTED] and [REDACTED] both failed"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, redactNames(tc.input))
		})
	}
}
