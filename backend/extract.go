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
}

func newGPTExtractor() (*gptExtractor, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &gptExtractor{client: openai.NewClient(key)}, nil
}

func (e *gptExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	systemPrompt := buildExtractionPrompt(req.Classes)

	resp, err := e.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
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

func buildExtractionPrompt(classes []ClassGroup) string {
	var sb strings.Builder
	sb.WriteString(`You are a teaching assistant analyzing a teacher's audio transcript about student observations.

Your task:
1. Identify which students are mentioned in the transcript
2. Match each mentioned name to the student roster below (handle phonetic/partial matches)
3. Extract the date if mentioned (format YYYY-MM-DD), otherwise leave empty
4. Extract relevant passages from the transcript that mention or describe this student
   - Include direct quotes where the teacher discusses this student
   - Preserve the teacher's exact wording and tone
   - Include 1-3 key passages per student, separated by " | " if multiple
   - Do NOT rewrite, summarize, or paraphrase - use the teacher's original language

Student Roster:
`)
	for _, c := range classes {
		for _, s := range c.Students {
			sb.WriteString(fmt.Sprintf("- %s (class %s)\n", s.Name, c.Name))
		}
	}

	sb.WriteString(`
Rules:
- Match mentioned names against the roster even if pronunciation differs slightly
- Set confidence 0.0-1.0 for each match. Use >= 0.7 for confident matches.
- If confidence < 0.7, include up to 3 closest roster matches in "candidates"
- Extract quoted_text directly from the transcript - preserve the teacher's exact words and emotion
- If the transcript contains observations about "everyone", "all students", "the class", or similar group references, include those statements ONLY for students who are also individually mentioned by name in the transcript
- Do NOT create entries for students who are only covered by group observations but never mentioned individually
- For individually mentioned students, combine their specific observations with any relevant group-level observations
- If the transcript contains group references like "everyone", "all students", or "the class", apply those observations only to students in the class being discussed, not to ALL classes. Use context clues (class name mentions, prior student mentions) to determine which class is meant.
- For multi-student transcripts, produce a separate entry per student with relevant passages
- If a mentioned student cannot be matched to any roster entry, do not include them in the output
- If no students are clearly mentioned, return an empty students array
- The "class" field for each student MUST exactly match one of the class names from the roster above. Do not invent or abbreviate class names.
- IMPORTANT: Never modify, clean up, or formally rewrite the teacher's text. Always preserve their original voice.
`)
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
