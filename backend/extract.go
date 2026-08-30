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
	Date     string           `json:"date"`
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
		Schema:       extractResponseSchema(req.Classes),
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
// class_name is constrained to an enum of the roster's actual class names
// (from classes) so the model is structurally forced to pick a real class,
// rather than relying on the prompt instruction alone. If classes is empty
// (schema-shape tests, or a live extraction whose roster read failed — see
// voice_note_process.go), class_name falls back to a plain string so the
// schema never demands an unsatisfiable enum.
func extractResponseSchema(classes []ClassGroup) json.RawMessage {
	classNames := make([]string, 0, len(classes))
	for _, c := range classes {
		classNames = append(classNames, c.Name)
	}

	classNameSchema := map[string]any{"type": "string"}
	if len(classNames) > 0 {
		classNameSchema["enum"] = classNames
	}

	candidateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"class_name": classNameSchema,
		},
		"required":             []string{"name", "class_name"},
		"additionalProperties": false,
	}

	studentSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"class_name":  classNameSchema,
			"quoted_text": map[string]any{"type": "string"},
			"confidence":  map[string]any{"type": "number"},
			"candidates": map[string]any{
				"type":  "array",
				"items": candidateSchema,
			},
		},
		"required":             []string{"name", "class_name", "quoted_text", "confidence", "candidates"},
		"additionalProperties": false,
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"students": map[string]any{
				"type":  "array",
				"items": studentSchema,
			},
			"date": map[string]any{"type": "string"},
		},
		"required":             []string{"students", "date"},
		"additionalProperties": false,
	}

	b, err := json.Marshal(schema)
	if err != nil {
		// Construction is fully static/programmatic; a marshal error here
		// means a bug in this function, not a runtime condition to handle.
		panic(fmt.Sprintf("extractResponseSchema: %v", err))
	}
	return json.RawMessage(b)
}
