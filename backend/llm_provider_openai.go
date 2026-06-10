// llm_provider_openai.go implements LLMProvider backed by OpenAI.
package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func (p *openaiProvider) ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (string, error) {
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
		return "", fmt.Errorf("openai chat json failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	raw := resp.Choices[0].Message.Content
	if parseErr := json.Unmarshal([]byte(raw), out); parseErr != nil {
		return "", fmt.Errorf("failed to parse extraction response: %w", parseErr)
	}
	return raw, nil
}

func (p *openaiProvider) ChatText(ctx context.Context, req ChatTextRequest) (string, error) {
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: p.models[LLMTaskReport],
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: req.UserPrompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai chat text failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func (p *openaiProvider) Vision(ctx context.Context, req VisionRequest, out any) (string, error) {
	model := p.models[LLMTaskVision]
	b64 := encodeImageBase64(req.ImageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", req.MediaType, b64)

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleUser,
				MultiContent: []openai.ChatMessagePart{
					{Type: openai.ChatMessagePartTypeText, Text: req.Prompt},
					{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL:    dataURL,
							Detail: openai.ImageURLDetailHigh,
						},
					},
				},
			},
		},
		MaxCompletionTokens: 4096,
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
		return "", fmt.Errorf("openai vision failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai vision returned no choices")
	}
	raw := resp.Choices[0].Message.Content
	if parseErr := json.Unmarshal([]byte(raw), out); parseErr != nil {
		return "", fmt.Errorf("failed to parse vision response: %w", parseErr)
	}
	return raw, nil
}

func (p *openaiProvider) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error) {
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
	})
	if err != nil {
		return TranscribeResponse{}, fmt.Errorf("whisper transcription failed: %w", err)
	}
	return TranscribeResponse{Text: resp.Text}, nil
}

// encodeImageBase64 encodes image bytes as base64.
func encodeImageBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
