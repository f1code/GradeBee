// llm_provider.go defines the LLMProvider abstraction that backs all LLM call
// sites (extraction, report generation, transcription). Two production
// implementations exist: openaiProvider and mistralProvider.
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

// Per-operation deadlines applied at the provider boundary so callers never
// wait indefinitely on a hung upstream. Each provider method wraps its ctx
// with context.WithTimeout; a caller ctx that already carries a shorter
// deadline wins naturally.
const (
	// llmChatTimeout bounds ChatJSON and ChatText calls.
	llmChatTimeout = 120 * time.Second
	// llmTranscribeTimeout bounds Transcribe calls, which upload audio and
	// can legitimately run for several minutes.
	llmTranscribeTimeout = 300 * time.Second
)

// LLMTask identifies a specific use case for a model selection lookup.
type LLMTask string

const (
	LLMTaskExtraction    LLMTask = "extraction"
	LLMTaskReport        LLMTask = "report"
	LLMTaskTranscription LLMTask = "transcription"
)

// ChatJSONRequest is input to a structured-JSON chat call.
type ChatJSONRequest struct {
	SystemPrompt string
	UserPrompt   string
	SchemaName   string
	Schema       json.RawMessage
}

// ChatTextRequest is input to a free-form text chat call.
type ChatTextRequest struct {
	UserPrompt string
}

// TranscribeRequest is input to an audio transcription call.
type TranscribeRequest struct {
	Filename    string
	Audio       io.Reader
	ContextBias []string
}

// LLMResponse is the output of a provider call. For ChatJSON, Text is the raw JSON.
type LLMResponse struct {
	Text string
	// Usage is nil when no response arrived. A provider sets it, even alongside
	// an error, once a response came back: a reply that failed to decode still
	// cost money.
	Usage *LLMUsage
}

// LLMUsage is what a call billed. Chat fills tokens; transcription fills
// AudioSeconds, 0 when the provider does not report it.
type LLMUsage struct {
	InputTokens  int
	OutputTokens int
	AudioSeconds int
}

// LLMProvider abstracts a single LLM backend (OpenAI or Mistral).
type LLMProvider interface {
	// Name returns the provider identifier, e.g. "openai" or "mistral".
	Name() string
	// Model returns the configured model ID for a given task.
	Model(task LLMTask) string
	// ChatJSON calls the provider for a structured JSON response and unmarshals
	// the result into out.
	ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (LLMResponse, error)
	// ChatText calls the provider for a free-form text response.
	ChatText(ctx context.Context, req ChatTextRequest) (LLMResponse, error)
	// Transcribe converts audio to text with optional context bias terms.
	Transcribe(ctx context.Context, req TranscribeRequest) (LLMResponse, error)
}

// Sentry metric names for the provider boundary. Named for the call, not the
// model's output quality — nothing here measures whether the answer was good.
const (
	metricLLMCallDuration = "llm.call.duration"
	metricLLMCallCount    = "llm.call.count"
	metricLLMCallErrors   = "llm.call.errors"
)

// instrumentedProvider wraps an LLMProvider to emit Sentry metrics (call
// latency, call count, and errors split by kind) and an llm_calls row at the
// provider boundary. Wrapping here, rather than in each concrete provider,
// covers openaiProvider and mistralProvider identically without touching
// either. Metrics no-op automatically when Sentry is not initialised:
// sentry.NewMeter returns a noop meter whenever no client is bound to the hub
// (see TestRecordLLMCall_NoopWhenSentryUninitialised).
type instrumentedProvider struct {
	LLMProvider
	db *sql.DB
}

func instrumentProvider(p LLMProvider, db *sql.DB) LLMProvider {
	return &instrumentedProvider{LLMProvider: p, db: db}
}

func (p *instrumentedProvider) ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (LLMResponse, error) {
	start := time.Now()
	resp, err := p.LLMProvider.ChatJSON(ctx, req, out)
	recordLLMCall(ctx, p.LLMProvider, LLMTaskExtraction, start, err)
	p.saveLLMCall(ctx, LLMTaskExtraction, resp.Usage, err)
	return resp, err
}

func (p *instrumentedProvider) ChatText(ctx context.Context, req ChatTextRequest) (LLMResponse, error) {
	start := time.Now()
	resp, err := p.LLMProvider.ChatText(ctx, req)
	recordLLMCall(ctx, p.LLMProvider, LLMTaskReport, start, err)
	p.saveLLMCall(ctx, LLMTaskReport, resp.Usage, err)
	return resp, err
}

func (p *instrumentedProvider) Transcribe(ctx context.Context, req TranscribeRequest) (LLMResponse, error) {
	start := time.Now()
	resp, err := p.LLMProvider.Transcribe(ctx, req)
	recordLLMCall(ctx, p.LLMProvider, LLMTaskTranscription, start, err)
	p.saveLLMCall(ctx, LLMTaskTranscription, resp.Usage, err)
	return resp, err
}

type llmCaller struct{ userID, traceID string }

// withLLMCaller names who AI calls on ctx bill to, for the llm_calls row.
// traceID may be empty.
func withLLMCaller(ctx context.Context, userID, traceID string) context.Context {
	return context.WithValue(ctx, llmCallerKey, llmCaller{userID: userID, traceID: traceID})
}

// saveLLMCall inserts the llm_calls row for a call that got a response. It
// never fails the call: the result already cost money and the caller needs it.
func (p *instrumentedProvider) saveLLMCall(ctx context.Context, task LLMTask, usage *LLMUsage, callErr error) {
	if usage == nil {
		return
	}
	log := loggerFromContext(ctx)
	caller, ok := ctx.Value(llmCallerKey).(llmCaller)
	if !ok || caller.userID == "" {
		log.Warn("llm call not recorded: no user on context", "task", task)
		return
	}
	var inputTokens, outputTokens, audioSeconds any
	if task == LLMTaskTranscription {
		audioSeconds = usage.AudioSeconds
	} else {
		inputTokens, outputTokens = usage.InputTokens, usage.OutputTokens
	}
	// WithoutCancel: a client gone mid-report still got billed.
	_, err := p.db.ExecContext(context.WithoutCancel(ctx), `
		INSERT INTO llm_calls (user_id, trace_id, provider, model, task, input_tokens, output_tokens, audio_seconds, ok)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		caller.userID, sql.NullString{String: caller.traceID, Valid: caller.traceID != ""},
		p.Name(), p.Model(task), string(task), inputTokens, outputTokens, audioSeconds, callErr == nil)
	if err != nil {
		log.Warn("llm call not recorded", "task", task, "error", err)
	}
}

// recordLLMCall emits call duration, call count, and (on failure) an error
// count split by kind, tagged by task/model/provider. PII-free by
// construction: only durations, counts, model IDs, and task names.
func recordLLMCall(ctx context.Context, p LLMProvider, task LLMTask, start time.Time, err error) {
	meter := sentry.NewMeter(ctx)
	attrs := sentry.WithAttributes(
		attribute.String("task", string(task)),
		attribute.String("model", p.Model(task)),
		attribute.String("provider", p.Name()),
	)

	meter.Distribution(metricLLMCallDuration, float64(time.Since(start).Milliseconds()), sentry.WithUnit(sentry.UnitMillisecond), attrs)
	meter.Count(metricLLMCallCount, 1, attrs)

	if err != nil {
		kind := "other"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			kind = "deadline_exceeded"
		case errors.Is(err, context.Canceled):
			kind = "canceled"
		}
		meter.Count(metricLLMCallErrors, 1, attrs, sentry.WithAttributes(attribute.String("kind", kind)))
	}
}

// defaultModels returns sensible default model IDs for each provider + task
// combination when the corresponding env var is not set.
func defaultModels(provider string) map[LLMTask]string {
	switch provider {
	case "openai":
		return map[LLMTask]string{
			LLMTaskExtraction:    "gpt-5.4-mini",
			LLMTaskReport:        "gpt-5.4-mini",
			LLMTaskTranscription: "whisper-1",
		}
	default: // "mistral"
		return map[LLMTask]string{
			LLMTaskExtraction:    "mistral-medium-2508",
			LLMTaskReport:        "mistral-medium-2508",
			LLMTaskTranscription: "voxtral-mini-latest",
		}
	}
}

// resolveModels reads per-task model env vars, falling back to defaults.
func resolveModels(provider string) map[LLMTask]string {
	m := defaultModels(provider)
	if v := os.Getenv("LLM_MODEL_EXTRACTION"); v != "" {
		m[LLMTaskExtraction] = v
	}
	if v := os.Getenv("LLM_MODEL_REPORT"); v != "" {
		m[LLMTaskReport] = v
	}
	if v := os.Getenv("LLM_MODEL_TRANSCRIPTION"); v != "" {
		m[LLMTaskTranscription] = v
	}
	return m
}

// LoadProvider reads LLM_PROVIDER from the environment, validates the active
// provider's API key, and returns the configured LLMProvider. It is called
// from NewProdDeps so the binary fails to start on misconfiguration. db
// receives the llm_calls rows.
func LoadProvider(db *sql.DB) (LLMProvider, error) {
	providerName := os.Getenv("LLM_PROVIDER")
	if providerName == "" {
		providerName = "mistral"
	}

	models := resolveModels(providerName)

	var p LLMProvider
	switch providerName {
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("LLM_PROVIDER=openai but OPENAI_API_KEY is not set")
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		p = newOpenAIProvider(key, baseURL, models)
	case "mistral":
		key := os.Getenv("MISTRAL_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("LLM_PROVIDER=mistral but MISTRAL_API_KEY is not set")
		}
		baseURL := os.Getenv("MISTRAL_BASE_URL")
		p = newMistralProvider(key, baseURL, models)
	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q: must be \"openai\" or \"mistral\"", providerName)
	}

	slog.Info("LLM provider loaded",
		"provider", p.Name(),
		"extraction", p.Model(LLMTaskExtraction),
		"report", p.Model(LLMTaskReport),
		"transcription", p.Model(LLMTaskTranscription),
	)
	return instrumentProvider(p, db), nil
}
