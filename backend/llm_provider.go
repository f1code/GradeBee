// llm_provider.go defines the LLMProvider abstraction that backs all LLM call
// sites (extraction, report generation, vision, transcription). Two production
// implementations exist: openaiProvider and mistralProvider.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
)

// LLMTask identifies a specific use case for a model selection lookup.
type LLMTask string

const (
	LLMTaskExtraction    LLMTask = "extraction"
	LLMTaskReport        LLMTask = "report"
	LLMTaskVision        LLMTask = "vision"
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

// VisionRequest is input to a multimodal vision call.
type VisionRequest struct {
	Prompt    string
	MediaType string // e.g. "image/jpeg"
	ImageData []byte // raw image bytes
	// JSON schema for structured output
	SchemaName string
	Schema     json.RawMessage
}

// TranscribeRequest is input to an audio transcription call.
type TranscribeRequest struct {
	Filename    string
	Audio       io.Reader
	ContextBias []string
}

// TranscribeResponse is the output of a transcription call.
type TranscribeResponse struct {
	Text string
}

// LLMProvider abstracts a single LLM backend (OpenAI or Mistral).
type LLMProvider interface {
	// Name returns the provider identifier, e.g. "openai" or "mistral".
	Name() string
	// Model returns the configured model ID for a given task.
	Model(task LLMTask) string
	// ChatJSON calls the provider for a structured JSON response and unmarshals
	// the result into out. It handles parse-and-retry internally.
	ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (rawJSON string, err error)
	// ChatText calls the provider for a free-form text response.
	ChatText(ctx context.Context, req ChatTextRequest) (string, error)
	// Vision calls the provider with an image+text prompt and unmarshals the
	// structured JSON response into out.
	Vision(ctx context.Context, req VisionRequest, out any) (rawJSON string, err error)
	// Transcribe converts audio to text with optional context bias terms.
	Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error)
}

// defaultModels returns sensible default model IDs for each provider + task
// combination when the corresponding env var is not set.
func defaultModels(provider string) map[LLMTask]string {
	switch provider {
	case "openai":
		return map[LLMTask]string{
			LLMTaskExtraction:    "gpt-5.4-mini",
			LLMTaskReport:        "gpt-5.4-mini",
			LLMTaskVision:        "gpt-5.4-mini",
			LLMTaskTranscription: "whisper-1",
		}
	default: // "mistral"
		return map[LLMTask]string{
			LLMTaskExtraction:    "mistral-medium-2508",
			LLMTaskReport:        "mistral-medium-2508",
			LLMTaskVision:        "mistral-medium-2508",
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
	if v := os.Getenv("LLM_MODEL_VISION"); v != "" {
		m[LLMTaskVision] = v
	}
	if v := os.Getenv("LLM_MODEL_TRANSCRIPTION"); v != "" {
		m[LLMTaskTranscription] = v
	}
	return m
}

// resolveRetries reads the per-provider JSON retry env var. Returns the fallback
// if the env var is unset or invalid.
func resolveRetries(envVar string, fallback int) int {
	v := os.Getenv(envVar)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// LoadProvider reads LLM_PROVIDER from the environment, validates the active
// provider's API key, and returns the configured LLMProvider. It is called
// from NewProdDeps so the binary fails to start on misconfiguration.
func LoadProvider() (LLMProvider, error) {
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
		retries := resolveRetries("LLM_JSON_RETRIES_OPENAI", 0)
		p = newOpenAIProvider(key, baseURL, models, retries)
	case "mistral":
		key := os.Getenv("MISTRAL_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("LLM_PROVIDER=mistral but MISTRAL_API_KEY is not set")
		}
		baseURL := os.Getenv("MISTRAL_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.mistral.ai/v1"
		}
		retries := resolveRetries("LLM_JSON_RETRIES_MISTRAL", 1)
		p = newMistralProvider(key, baseURL, models, retries)
	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q: must be \"openai\" or \"mistral\"", providerName)
	}

	slog.Info("LLM provider loaded",
		"provider", p.Name(),
		"extraction", p.Model(LLMTaskExtraction),
		"report", p.Model(LLMTaskReport),
		"vision", p.Model(LLMTaskVision),
		"transcription", p.Model(LLMTaskTranscription),
	)
	return p, nil
}
