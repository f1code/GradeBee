// extract.go defines the Extractor interface and its OpenAI GPT implementation.
// The extractor takes a transcript and student roster, returning structured
// per-student extraction results with fuzzy name matching and confidence scores.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// Extractor takes a transcript + student roster and returns structured extraction.
type Extractor interface {
	Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error)
}

// ExtractRequest is the input to an extraction call.
type ExtractRequest struct {
	Transcript string
	Classes    []ClassGroup
}

// ExtractResponse is the structured output from extraction.
type ExtractResponse struct {
	Students []MatchedStudent `json:"students"`
	Date string `json:"date"`
}

// MatchedStudent is a single student extraction result.
type MatchedStudent struct {
	Name       string             `json:"name"`
	Class      string             `json:"class"`
	QuotedText string             `json:"quoted_text"` // Extracted passages from transcript, unchanged
	Confidence float64            `json:"confidence"`
	Candidates []StudentCandidate `json:"candidates,omitempty"`
}

// StudentCandidate is a possible roster match for a low-confidence extraction.
type StudentCandidate struct {
	Name  string `json:"name"`
	Class string `json:"class"`
}

// gptExtractor uses OpenAI GPT to extract student mentions from transcripts.
type gptExtractor struct {
	client *openai.Client
	model  string // defaults to ProductionModelName
}

func newGPTExtractor() (*gptExtractor, error) {
	return newGPTExtractorWithModel(ProductionModelName)
}

// newGPTExtractorWithModel creates a gptExtractor with a specific model.
func newGPTExtractorWithModel(model string) (*gptExtractor, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &gptExtractor{client: openai.NewClient(key), model: model}, nil
}

func (e *gptExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	systemPrompt := BuildExtractionPrompt(req.Classes)

	resp, err := e.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: e.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: req.Transcript},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   "extract_response",
				Strict: true,
				Schema: extractResponseSchema(),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai extraction failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	var result ExtractResponse
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse extraction response: %w", err)
	}

	// Default date to today if not extracted.
	if result.Date == "" {
		result.Date = time.Now().Format("2006-01-02")
	}

	return &result, nil
}

func BuildExtractionPrompt(classes []ClassGroup) string {
	var sb strings.Builder
	sb.WriteString(extractionPromptPrefix)
	for _, c := range classes {
		for _, s := range c.Students {
			if len(s.Aliases) > 0 {
				sb.WriteString(fmt.Sprintf("- %s (aka %s) (class %s)\n", s.Name, strings.Join(s.Aliases, ", "), c.Name))
			} else {
				sb.WriteString(fmt.Sprintf("- %s (class %s)\n", s.Name, c.Name))
			}
		}
	}
	sb.WriteString(extractionPromptSuffix)
	return sb.String()
}

// extractResponseSchema returns the JSON schema for structured outputs.
func extractResponseSchema() json.RawMessage {
	schema := `{
		"type": "object",
		"properties": {
			"students": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string"},
						"class": {"type": "string"},
						"quoted_text": {"type": "string"},
						"confidence": {"type": "number"},
						"candidates": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"name": {"type": "string"},
									"class": {"type": "string"}
								},
								"required": ["name", "class"],
								"additionalProperties": false
							}
						}
					},
					"required": ["name", "class", "quoted_text", "confidence", "candidates"],
					"additionalProperties": false
				}
			},
			"date": {"type": "string"}
		},
		"required": ["students", "date"],
		"additionalProperties": false
	}`
	return json.RawMessage(schema)
}
