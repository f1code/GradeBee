// llm_provider_mistral.go implements LLMProvider backed by Mistral.
// Chat uses the OpenAI-compatible endpoint via go-openai.
// Transcription posts multipart to Voxtral's /audio/transcriptions directly.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"

	openai "github.com/sashabaranov/go-openai"
)

const mistralDefaultBaseURL = "https://api.mistral.ai/v1"

// mistralProvider wraps an OpenAI-compat client for chat and calls
// Voxtral transcription over plain HTTP.
type mistralProvider struct {
	chatClient *openai.Client
	apiKey     string
	baseURL    string
	models     map[LLMTask]string
}

func newMistralProvider(apiKey, baseURL string, models map[LLMTask]string) *mistralProvider {
	if baseURL == "" {
		baseURL = mistralDefaultBaseURL
	}
	// go-openai trims it for chat; transcription joins paths itself.
	baseURL = strings.TrimRight(baseURL, "/")

	// OpenAI-compat client pointed at Mistral.
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL

	return &mistralProvider{
		chatClient: openai.NewClientWithConfig(cfg),
		apiKey:     apiKey,
		baseURL:    baseURL,
		models:     models,
	}
}

func (p *mistralProvider) Name() string { return "mistral" }

func (p *mistralProvider) Model(task LLMTask) string { return p.models[task] }

func (p *mistralProvider) ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (LLMResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, llmChatTimeout)
	defer cancel()
	model := p.models[LLMTaskExtraction]
	resp, err := p.chatClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: req.SystemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: req.UserPrompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   req.SchemaName,
				Strict: true,
				Schema: req.Schema,
			},
		},
	})
	if err != nil {
		return LLMResponse{}, fmt.Errorf("mistral chat json failed: %w", err)
	}
	res := LLMResponse{Usage: chatUsage(resp)}
	if len(resp.Choices) == 0 {
		return res, fmt.Errorf("mistral returned no choices")
	}
	res.Text = resp.Choices[0].Message.Content
	if parseErr := json.Unmarshal([]byte(res.Text), out); parseErr != nil {
		return res, fmt.Errorf("failed to parse extraction response: %w", parseErr)
	}
	return res, nil
}

func (p *mistralProvider) ChatText(ctx context.Context, req ChatTextRequest) (LLMResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, llmChatTimeout)
	defer cancel()
	resp, err := p.chatClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: p.models[LLMTaskReport],
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: req.UserPrompt},
		},
	})
	if err != nil {
		return LLMResponse{}, fmt.Errorf("mistral chat text failed: %w", err)
	}
	res := LLMResponse{Usage: chatUsage(resp)}
	if len(resp.Choices) == 0 {
		return res, fmt.Errorf("mistral returned no choices")
	}
	res.Text = resp.Choices[0].Message.Content
	return res, nil
}

// sanitiseContextBias applies Voxtral's wire-format rules to a slice of raw
// class names:
//   - Replace runs of whitespace with "_"
//   - Drop commas
//   - Skip terms that become empty after sanitisation (slog WARN)
//   - De-dupe case-insensitively (preserve first occurrence)
//   - Cap at 100 terms
func sanitiseContextBias(terms []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, term := range terms {
		// Replace whitespace runs with underscore.
		sanitised := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return '_'
			}
			return r
		}, term)
		// Drop commas.
		sanitised = strings.ReplaceAll(sanitised, ",", "")
		// Collapse multiple underscores from adjacent whitespace.
		for strings.Contains(sanitised, "__") {
			sanitised = strings.ReplaceAll(sanitised, "__", "_")
		}
		// Trim leading/trailing underscores from the space replacement.
		sanitised = strings.Trim(sanitised, "_")

		if sanitised == "" {
			slog.Warn("mistral: context bias term dropped (empty after sanitisation)", "original", term)
			continue
		}
		key := strings.ToLower(sanitised)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, sanitised)
		if len(result) >= 100 {
			break
		}
	}
	return result
}

// Transcribe runs Voxtral transcription bounded by llmTranscribeTimeout.
// No retry: the user can retry the job (handleJobRetry).
func (p *mistralProvider) Transcribe(ctx context.Context, req TranscribeRequest) (LLMResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, llmTranscribeTimeout)
	defer cancel()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", filepath.Base(req.Filename))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("voxtral transcription failed: %w", err)
	}
	if _, err := io.Copy(part, req.Audio); err != nil {
		return LLMResponse{}, fmt.Errorf("voxtral transcription failed: read audio: %w", err)
	}
	err = w.WriteField("model", p.models[LLMTaskTranscription])
	for _, term := range sanitiseContextBias(req.ContextBias) {
		err = errors.Join(err, w.WriteField("context_bias[]", term))
	}
	if err := errors.Join(err, w.Close()); err != nil {
		return LLMResponse{}, fmt.Errorf("voxtral transcription failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/audio/transcriptions", body)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("voxtral transcription failed: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("voxtral transcription failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) //nolint:errcheck // best-effort error detail
		return LLMResponse{}, fmt.Errorf("voxtral transcription failed: status %d: %s", resp.StatusCode, msg)
	}

	// Schema: official Python SDK TranscriptionResponse / UsageInfo.
	var out struct {
		Text  string `json:"text"`
		Usage struct {
			PromptAudioSeconds int `json:"prompt_audio_seconds"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return LLMResponse{Usage: &LLMUsage{}}, fmt.Errorf("voxtral transcription failed: decode response: %w", err)
	}
	return LLMResponse{Text: out.Text, Usage: &LLMUsage{AudioSeconds: out.Usage.PromptAudioSeconds}}, nil
}
