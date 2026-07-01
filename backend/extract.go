// extract.go defines the Extractor interface and its LLM implementation.
// The extractor takes a transcript and student roster, returning structured
// per-student extraction results with fuzzy name matching and confidence scores.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Extractor takes a transcript + student roster and returns structured extraction.
type Extractor interface {
	Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error)
	// Model returns the model ID used for extraction (for stamping model_version).
	Model() string
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
	ClassName  string             `json:"class_name"`
	QuotedText string             `json:"quoted_text"` // Extracted passages from transcript, unchanged
	Confidence float64            `json:"confidence"`
	Candidates []StudentCandidate `json:"candidates,omitempty"`
}

// StudentCandidate is a possible roster match for a low-confidence extraction.
type StudentCandidate struct {
	Name      string `json:"name"`
	ClassName string `json:"class_name"`
}

// llmExtractor uses an LLMProvider to extract student mentions from transcripts.
type llmExtractor struct {
	provider LLMProvider
}

func newLLMExtractor(provider LLMProvider) *llmExtractor {
	return &llmExtractor{provider: provider}
}

func (e *llmExtractor) Model() string {
	return e.provider.Model(LLMTaskExtraction)
}

func (e *llmExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	systemPrompt := BuildExtractionPrompt(req.Classes)

	var result ExtractResponse
	_, err := e.provider.ChatJSON(ctx, ChatJSONRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   req.Transcript,
		SchemaName:   "extract_response",
		Schema:       extractResponseSchema(),
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w", err)
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
				sb.WriteString(fmt.Sprintf("- %s (aka %s) (class_name %s)\n", s.Name, strings.Join(s.Aliases, ", "), c.Name))
			} else {
				sb.WriteString(fmt.Sprintf("- %s (class_name %s)\n", s.Name, c.Name))
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
						"class_name": {"type": "string"},
						"quoted_text": {"type": "string"},
						"confidence": {"type": "number"},
						"candidates": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"name": {"type": "string"},
									"class_name": {"type": "string"}
								},
								"required": ["name", "class_name"],
								"additionalProperties": false
							}
						}
					},
					"required": ["name", "class_name", "quoted_text", "confidence", "candidates"],
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
