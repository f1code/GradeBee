// llm_provider_metrics_test.go covers the Sentry metrics emitted at the
// LLM provider boundary by instrumentProvider (task #97).
package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metricsFakeProvider is a minimal LLMProvider stub for exercising instrumentProvider
// without a network call. delay, if set, is slept before returning so the
// duration metric's value can be asserted against a known lower bound.
type metricsFakeProvider struct {
	name        string
	models      map[LLMTask]string
	delay       time.Duration
	chatJSONErr error
}

func (f *metricsFakeProvider) Name() string              { return f.name }
func (f *metricsFakeProvider) Model(task LLMTask) string { return f.models[task] }

func (f *metricsFakeProvider) ChatJSON(_ context.Context, _ ChatJSONRequest, _ any) (string, error) {
	time.Sleep(f.delay)
	return "", f.chatJSONErr
}
func (f *metricsFakeProvider) ChatText(_ context.Context, _ ChatTextRequest) (string, error) {
	return "", f.chatJSONErr
}
func (f *metricsFakeProvider) Vision(_ context.Context, _ VisionRequest, _ any) (string, error) {
	return "", f.chatJSONErr
}
func (f *metricsFakeProvider) Transcribe(_ context.Context, _ TranscribeRequest) (TranscribeResponse, error) {
	return TranscribeResponse{}, f.chatJSONErr
}

// metricsTestHub binds a fresh client + mock transport to a context, mirroring
// the setup sentry-go's own metrics tests use so NewMeter returns a real meter.
func metricsTestHub() (context.Context, *sentry.MockTransport, *sentry.Hub) {
	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@o0.ingest.sentry.io/0",
		Transport: transport,
	})
	if err != nil {
		panic(err)
	}
	hub := sentry.NewHub(client, sentry.NewScope())
	ctx := sentry.SetHubOnContext(context.Background(), hub)
	return ctx, transport, hub
}

func flushMetrics(hub *sentry.Hub) {
	hub.Flush(time.Second)
}

// metricByName returns the first metric with the given name across all
// captured events, and whether one was found.
func metricByName(transport *sentry.MockTransport, name string) (sentry.Metric, bool) {
	for _, ev := range transport.Events() {
		for _, m := range ev.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return sentry.Metric{}, false
}

func TestRecordLLMCall_NoopWhenSentryUninitialised(t *testing.T) {
	ctx, transport, hub := metricsTestHub()
	provider := &metricsFakeProvider{name: "mistral", models: map[LLMTask]string{LLMTaskExtraction: "mistral-medium-2508"}}

	// Presence: with a client bound, the call metric is emitted.
	recordLLMCall(ctx, provider, LLMTaskExtraction, time.Now(), nil)
	flushMetrics(hub)
	_, ok := metricByName(transport, metricLLMCallCount)
	require.True(t, ok, "sanity check: metric must be emitted when a client is bound")
	eventsWithClient := len(transport.Events())

	// Absence: once unbound (mirrors production when SENTRY_DSN is unset),
	// sentry.NewMeter must fall back to a noop meter — no panic, and no new
	// metric captured.
	hub.BindClient(nil)
	require.Nil(t, hub.Client())
	assert.NotPanics(t, func() {
		recordLLMCall(ctx, provider, LLMTaskExtraction, time.Now(), nil)
	})
	flushMetrics(hub)
	assert.Len(t, transport.Events(), eventsWithClient, "no new metric event once Sentry is uninitialised")
}

func TestInstrumentedProvider_EmitsDurationAndCount(t *testing.T) {
	ctx, transport, hub := metricsTestHub()
	provider := &metricsFakeProvider{
		name:   "openai",
		models: map[LLMTask]string{LLMTaskExtraction: "gpt-5.4-mini"},
		delay:  25 * time.Millisecond,
	}
	wrapped := instrumentProvider(provider)

	_, err := wrapped.ChatJSON(ctx, ChatJSONRequest{}, &struct{}{})
	require.NoError(t, err)
	flushMetrics(hub)

	dur, ok := metricByName(transport, metricLLMCallDuration)
	require.True(t, ok, "expected a %s metric", metricLLMCallDuration)
	assert.Equal(t, sentry.MetricTypeDistribution, dur.Type)
	assert.Equal(t, sentry.UnitMillisecond, dur.Unit)
	ms, isFloat := dur.Value.Float64()
	require.True(t, isFloat, "duration value should be a float64 in milliseconds")
	assert.GreaterOrEqual(t, ms, 25.0, "duration should reflect the actual ~25ms call, got %vms — wrong unit/scale would fail this bound", ms)
	assert.Less(t, ms, 5000.0, "duration should be in milliseconds, not seconds or nanoseconds")
	assert.Equal(t, "extraction", dur.Attributes["task"].AsString())
	assert.Equal(t, "gpt-5.4-mini", dur.Attributes["model"].AsString())
	assert.Equal(t, "openai", dur.Attributes["provider"].AsString())

	count, ok := metricByName(transport, metricLLMCallCount)
	require.True(t, ok, "expected a %s metric", metricLLMCallCount)
	assert.Equal(t, sentry.MetricTypeCounter, count.Type)
	got, _ := count.Value.Int64()
	assert.Equal(t, int64(1), got)
	assert.Equal(t, "extraction", count.Attributes["task"].AsString())

	_, hasErrors := metricByName(transport, metricLLMCallErrors)
	assert.False(t, hasErrors, "no error metric expected on a successful call")
}

func TestInstrumentedProvider_VisionTask(t *testing.T) {
	ctx, transport, hub := metricsTestHub()
	provider := &metricsFakeProvider{name: "openai", models: map[LLMTask]string{LLMTaskVision: "gpt-5.4-mini"}}
	wrapped := instrumentProvider(provider)

	_, err := wrapped.Vision(ctx, VisionRequest{}, &struct{}{})
	require.NoError(t, err)
	flushMetrics(hub)

	count, ok := metricByName(transport, metricLLMCallCount)
	require.True(t, ok, "expected a %s metric", metricLLMCallCount)
	assert.Equal(t, "vision", count.Attributes["task"].AsString())
	assert.Equal(t, "gpt-5.4-mini", count.Attributes["model"].AsString())
}

func TestInstrumentedProvider_ErrorKindDeadlineExceeded(t *testing.T) {
	ctx, transport, hub := metricsTestHub()
	provider := &metricsFakeProvider{
		name:        "mistral",
		models:      map[LLMTask]string{LLMTaskReport: "mistral-medium-2508"},
		chatJSONErr: fmt.Errorf("mistral chat text failed: %w", context.DeadlineExceeded),
	}
	wrapped := instrumentProvider(provider)

	_, err := wrapped.ChatText(ctx, ChatTextRequest{})
	require.Error(t, err)
	flushMetrics(hub)

	errMetric, ok := metricByName(transport, metricLLMCallErrors)
	require.True(t, ok, "expected a %s metric", metricLLMCallErrors)
	assert.Equal(t, "deadline_exceeded", errMetric.Attributes["kind"].AsString())
	assert.Equal(t, "report", errMetric.Attributes["task"].AsString())
}

func TestInstrumentedProvider_ErrorKindCanceled(t *testing.T) {
	ctx, transport, hub := metricsTestHub()
	provider := &metricsFakeProvider{
		name:        "openai",
		models:      map[LLMTask]string{LLMTaskExtraction: "gpt-5.4-mini"},
		chatJSONErr: fmt.Errorf("openai chat json failed: %w", context.Canceled),
	}
	wrapped := instrumentProvider(provider)

	_, err := wrapped.ChatJSON(ctx, ChatJSONRequest{}, &struct{}{})
	require.Error(t, err)
	flushMetrics(hub)

	errMetric, ok := metricByName(transport, metricLLMCallErrors)
	require.True(t, ok, "expected a %s metric", metricLLMCallErrors)
	assert.Equal(t, "canceled", errMetric.Attributes["kind"].AsString())
}

func TestInstrumentedProvider_ErrorKindOther(t *testing.T) {
	ctx, transport, hub := metricsTestHub()
	provider := &metricsFakeProvider{
		name:        "mistral",
		models:      map[LLMTask]string{LLMTaskTranscription: "voxtral-mini-latest"},
		chatJSONErr: errors.New("boom"),
	}
	wrapped := instrumentProvider(provider)

	_, err := wrapped.Transcribe(ctx, TranscribeRequest{})
	require.Error(t, err)
	flushMetrics(hub)

	errMetric, ok := metricByName(transport, metricLLMCallErrors)
	require.True(t, ok, "expected a %s metric", metricLLMCallErrors)
	assert.Equal(t, "other", errMetric.Attributes["kind"].AsString())
	assert.Equal(t, "transcription", errMetric.Attributes["task"].AsString())
}

// TestLoadProvider_WrapsWithInstrumentation guards against a future refactor
// silently dropping the instrumentProvider() call in LoadProvider — every
// other test in this file constructs the wrapper directly and would stay
// green even if production wiring were deleted.
func TestLoadProvider_WrapsWithInstrumentation(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "mistral")
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("OPENAI_API_KEY", "")
	// resolveModels reads these regardless of provider; clear them so the
	// assertion below reflects defaultModels(), not a developer's local env.
	t.Setenv("LLM_MODEL_EXTRACTION", "")
	t.Setenv("LLM_MODEL_REPORT", "")
	t.Setenv("LLM_MODEL_VISION", "")
	t.Setenv("LLM_MODEL_TRANSCRIPTION", "")

	p, err := LoadProvider()
	require.NoError(t, err)

	_, ok := p.(*instrumentedProvider)
	require.True(t, ok, "LoadProvider() must return an *instrumentedProvider, got %T", p)
	assert.Equal(t, "mistral", p.Name())
	assert.Equal(t, "mistral-medium-2508", p.Model(LLMTaskExtraction))
}
