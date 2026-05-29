// llm_provider_mistral.go implements LLMProvider backed by Mistral.
// Chat and vision use the OpenAI-compatible endpoint via go-openai.
// Transcription uses the ZaguanLabs mistral-go/v2/sdk for Voxtral support.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	mistralSDK "github.com/ZaguanLabs/mistral-go/v2/sdk"
	openai "github.com/sashabaranov/go-openai"
)

const mistralDefaultBaseURL = "https://api.mistral.ai/v1"

// mistralProvider wraps an OpenAI-compat client for chat/vision and the
// ZaguanLabs SDK for Voxtral transcription.
type mistralProvider struct {
	chatClient    *openai.Client
	audioClient   *mistralSDK.MistralClient
	models        map[LLMTask]string
	jsonRetries   int
}

func newMistralProvider(apiKey, baseURL string, models map[LLMTask]string, retries int) *mistralProvider {
	if baseURL == "" {
		baseURL = mistralDefaultBaseURL
	}

	// OpenAI-compat client pointed at Mistral.
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL

	// ZaguanLabs client for Voxtral transcription.
	audioClient := mistralSDK.NewMistralClientDefault(apiKey)

	return &mistralProvider{
		chatClient:  openai.NewClientWithConfig(cfg),
		audioClient: audioClient,
		models:      models,
		jsonRetries: retries,
	}
}

func (p *mistralProvider) Name() string { return "mistral" }

func (p *mistralProvider) Model(task LLMTask) string { return p.models[task] }

func (p *mistralProvider) ChatJSON(ctx context.Context, req ChatJSONRequest, out any) (string, error) {
	model := p.models[LLMTaskExtraction]
	raw, err := p.chatJSONOnce(ctx, model, req.SystemPrompt, req.UserPrompt, req.SchemaName, req.Schema)
	if err != nil {
		return "", err
	}
	if parseErr := json.Unmarshal([]byte(raw), out); parseErr != nil {
		for i := 0; i < p.jsonRetries; i++ {
			slog.Info("mistral: JSON parse failed, retrying", "attempt", i+1, "error", parseErr)
			retryPrompt := req.UserPrompt + "\nYour previous response was not valid JSON for the schema. Return only valid JSON matching the schema."
			raw, err = p.chatJSONOnce(ctx, model, req.SystemPrompt, retryPrompt, req.SchemaName, req.Schema)
			if err != nil {
				return "", err
			}
			if parseErr2 := json.Unmarshal([]byte(raw), out); parseErr2 == nil {
				return raw, nil
			} else {
				slog.Error("mistral: JSON parse failed after retry", "attempt", i+1, "error", parseErr2)
				parseErr = parseErr2
			}
		}
		return "", fmt.Errorf("failed to parse extraction response: %w", parseErr)
	}
	return raw, nil
}

func (p *mistralProvider) chatJSONOnce(ctx context.Context, model, systemPrompt, userPrompt, schemaName string, schema json.RawMessage) (string, error) {
	resp, err := p.chatClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   schemaName,
				Strict: true,
				Schema: schema,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("mistral chat json failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("mistral returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func (p *mistralProvider) ChatText(ctx context.Context, req ChatTextRequest) (string, error) {
	resp, err := p.chatClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: p.models[LLMTaskReport],
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: req.UserPrompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("mistral chat text failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("mistral returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func (p *mistralProvider) Vision(ctx context.Context, req VisionRequest, out any) (string, error) {
	model := p.models[LLMTaskVision]
	b64 := encodeImageBase64(req.ImageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", req.MediaType, b64)

	resp, err := p.chatClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
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
		return "", fmt.Errorf("mistral vision failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("mistral vision returned no choices")
	}
	raw := resp.Choices[0].Message.Content

	// Try JSON parse with optional retry.
	if parseErr := json.Unmarshal([]byte(raw), out); parseErr != nil {
		for i := 0; i < p.jsonRetries; i++ {
			slog.Info("mistral: vision JSON parse failed, retrying", "attempt", i+1, "error", parseErr)
			retryPrompt := req.Prompt + "\nYour previous response was not valid JSON for the schema. Return only valid JSON matching the schema."
			retryReq := req
			retryReq.Prompt = retryPrompt
			raw2, err2 := p.Vision(ctx, retryReq, out)
			if err2 == nil {
				return raw2, nil
			}
		}
		return "", fmt.Errorf("failed to parse vision response: %w", parseErr)
	}
	return raw, nil
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

func (p *mistralProvider) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error) {
	bias := sanitiseContextBias(req.ContextBias)
	model := p.models[LLMTaskTranscription]

	resp, err := p.audioClient.Transcribe(model, req.Audio, req.Filename, &mistralSDK.TranscriptionRequest{
		ContextBias: bias,
	})
	if err != nil {
		return TranscribeResponse{}, fmt.Errorf("voxtral transcription failed: %w", err)
	}
	return TranscribeResponse{Text: resp.Text}, nil
}
