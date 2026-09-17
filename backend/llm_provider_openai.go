// llm_provider_openai.go implements LLMProvider backed by OpenAI.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// openaiProvider wraps the sashabaranov go-openai client.
type openaiProvider struct {
	client *openai.Client
	models map[LLMTask]string
}

func newOpenAIProvider(apiKey, baseURL string, models map[LLMTask]string) *openaiProvider {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &openaiProvider{
		client: openai.NewClientWithConfig(cfg),
		models: models,
	}
}

func (p *openaiProvider) Name() string { return "openai" }

func (p *openaiProvider) Model(task LLMTask) string { return p.models[task] }

func (p *openaiProvider) ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (LLMResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, llmChatTimeout)
	defer cancel()
	model := p.models[LLMTaskExtraction]
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
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
		return LLMResponse{}, fmt.Errorf("openai chat json failed: %w", err)
	}
	res := LLMResponse{Usage: chatUsage(resp)}
	if len(resp.Choices) == 0 {
		return res, fmt.Errorf("openai returned no choices")
	}
	res.Text = resp.Choices[0].Message.Content
	if parseErr := json.Unmarshal([]byte(res.Text), out); parseErr != nil {
		return res, fmt.Errorf("failed to parse extraction response: %w", parseErr)
	}
	return res, nil
}

func (p *openaiProvider) ChatText(ctx context.Context, req ChatTextRequest) (LLMResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, llmChatTimeout)
	defer cancel()
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: p.models[LLMTaskReport],
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: req.UserPrompt},
		},
	})
	if err != nil {
		return LLMResponse{}, fmt.Errorf("openai chat text failed: %w", err)
	}
	res := LLMResponse{Usage: chatUsage(resp)}
	if len(resp.Choices) == 0 {
		return res, fmt.Errorf("openai returned no choices")
	}
	res.Text = resp.Choices[0].Message.Content
	return res, nil
}

func (p *openaiProvider) Transcribe(ctx context.Context, req TranscribeRequest) (LLMResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, llmTranscribeTimeout)
	defer cancel()
	// OpenAI Whisper: pass context bias as a comma-separated prompt string.
	var prompt string
	if len(req.ContextBias) > 0 {
		prompt = "Classes: " + strings.Join(req.ContextBias, ", ")
	}
	resp, err := p.client.CreateTranscription(ctx, openai.AudioRequest{
		Model:    p.models[LLMTaskTranscription],
		FilePath: req.Filename,
		Reader:   req.Audio,
		Prompt:   prompt,
		// verbose_json carries duration, which OpenAI bills by.
		Format: openai.AudioResponseFormatVerboseJSON,
	})
	if err != nil {
		return LLMResponse{}, fmt.Errorf("whisper transcription failed: %w", err)
	}
	return LLMResponse{Text: resp.Text, Usage: &LLMUsage{AudioSeconds: int(math.Round(resp.Duration))}}, nil
}

// chatUsage reads token usage from a go-openai chat response; Mistral's
// OpenAI-compat endpoint fills the same fields.
func chatUsage(resp openai.ChatCompletionResponse) *LLMUsage {
	return &LLMUsage{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens}
}
