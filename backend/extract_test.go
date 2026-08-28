package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractPreservesTeacherVoice verifies that extracted QuotedText preserves
// the teacher's original language and emotion, not rewritten summaries.
func TestExtractPreservesTeacherVoice(t *testing.T) {
	provider := requireLiveLLM(t)
	extractor := newLLMExtractor(provider)

	// Example: raw teacher notes with strong emotion
	transcript := `Thursday. Maxence was impossibly bad today. I'm ready to choke the living 
sh*t out of him. He wouldn't stop talking during the lesson. 
Amara was great - very attentive and helpful to other students.`

	req := ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{
				Name: "Period 3",
				Students: []ClassStudent{
					{Name: "Maxence"},
					{Name: "Amara"},
				},
			},
		},
	}

	result, err := extractor.Extract(context.Background(), req)
	require.NoError(t, err, "Extract failed")
	require.Len(t, result.Students, 2, "Expected 2 students")

	// Find Maxence
	var maxence *MatchedStudent
	for i := range result.Students {
		if result.Students[i].Name == "Maxence" {
			maxence = &result.Students[i]
			break
		}
	}
	require.NotNil(t, maxence, "Maxence not found in extraction")

	// Verify QuotedText contains original phrasing, not formal rewrite
	assert.NotEmpty(t, maxence.QuotedText, "QuotedText is empty")

	// The quoted text should contain evidence of teacher's original voice
	// (not formal language like "had a very difficult day")
	assert.True(t, contains(maxence.QuotedText, "impossibly bad"),
		"QuotedText does not preserve original phrasing. Got: %s", maxence.QuotedText)
}

// TestExtractGroupObservations verifies that group-level observations
// are included for individually mentioned students but do NOT create
// entries for unmentioned students.
func TestExtractGroupObservations(t *testing.T) {
	provider := requireLiveLLM(t)
	extractor := newLLMExtractor(provider)

	transcript := `Today the class was way too loud and unfocused. Everyone was talking over each other. 
Specific note: Tommy helped me organize the materials, which was great.`

	req := ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{
				Name: "Period 1",
				Students: []ClassStudent{
					{Name: "Tommy"},
					{Name: "Lisa"},
				},
			},
		},
	}

	result, err := extractor.Extract(context.Background(), req)
	require.NoError(t, err, "Extract failed")

	// Only Tommy should be extracted — Lisa is not individually mentioned
	if len(result.Students) != 1 {
		names := make([]string, len(result.Students))
		for i, s := range result.Students {
			names[i] = s.Name
		}
		t.Fatalf("Expected 1 student (Tommy only), got %d: %v", len(result.Students), names)
	}

	tommy := result.Students[0]
	require.Equal(t, "Tommy", tommy.Name)

	// Tommy's QuotedText should include both his individual mention and the group observation
	assert.True(t, contains(tommy.QuotedText, "organize"),
		"Tommy QuotedText missing individual observation. Got: %s", tommy.QuotedText)
	assert.True(t, contains(tommy.QuotedText, "too loud") || contains(tommy.QuotedText, "unfocused") || contains(tommy.QuotedText, "talking over"),
		"Tommy QuotedText missing group observation. Got: %s", tommy.QuotedText)
}

// TestExtractGroupObservationsMultiClass verifies that group-level observations
// are scoped to the class being discussed, not applied across all classes.
func TestExtractGroupObservationsMultiClass(t *testing.T) {
	provider := requireLiveLLM(t)
	extractor := newLLMExtractor(provider)

	transcript := `Period 1 notes: Tommy was great today, really focused. The whole class was loud though.
Period 2 notes: Sarah did an amazing presentation on volcanoes.`

	req := ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{
				Name: "Period 1",
				Students: []ClassStudent{
					{Name: "Tommy"},
					{Name: "Lisa"},
				},
			},
			{
				Name: "Period 2",
				Students: []ClassStudent{
					{Name: "Sarah"},
					{Name: "Jake"},
				},
			},
		},
	}

	result, err := extractor.Extract(context.Background(), req)
	require.NoError(t, err, "Extract failed")

	// Only Tommy and Sarah should be extracted (individually mentioned).
	// Lisa and Jake are not mentioned by name.
	nameSet := make(map[string]MatchedStudent)
	for _, s := range result.Students {
		nameSet[s.Name] = s
	}

	assert.Contains(t, nameSet, "Tommy", "Tommy should be extracted (individually mentioned)")
	assert.Contains(t, nameSet, "Sarah", "Sarah should be extracted (individually mentioned)")
	assert.NotContains(t, nameSet, "Lisa", "Lisa should NOT be extracted (not individually mentioned)")
	assert.NotContains(t, nameSet, "Jake", "Jake should NOT be extracted (not individually mentioned)")

	// Tommy should have the group observation about the class being loud
	tommy := nameSet["Tommy"]
	assert.True(t, contains(tommy.QuotedText, "loud"),
		"Tommy should include Period 1 group observation about loudness. Got: %s", tommy.QuotedText)

	// Sarah should NOT have the "loud" group observation — that was about Period 1
	sarah := nameSet["Sarah"]
	assert.False(t, contains(sarah.QuotedText, "loud"),
		"Sarah should NOT have Period 1's group observation. Got: %s", sarah.QuotedText)
}

// TestExtractAmbiguousClassLowConfidence verifies that when a mentioned
// student's name exists in more than one class and the transcript gives no
// clue which class is meant, the extractor reports low confidence for that
// entry (below the 0.5 auto-create threshold in voice_note_process.go)
// rather than confidently picking one of the real-but-guessed classes.
// This guards against extractResponseSchema's class_name enum (task #79)
// silently forcing a wrong-but-valid class pick.
func TestExtractAmbiguousClassLowConfidence(t *testing.T) {
	provider := requireLiveLLM(t)
	extractor := newLLMExtractor(provider)

	transcript := `Alex did great work on the project today.`

	req := ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{
				Name: "Period 1",
				Students: []ClassStudent{
					{Name: "Alex"},
				},
			},
			{
				Name: "Period 2",
				Students: []ClassStudent{
					{Name: "Alex"},
				},
			},
		},
	}

	result, err := extractor.Extract(context.Background(), req)
	require.NoError(t, err, "Extract failed")

	for _, s := range result.Students {
		if s.Name != "Alex" {
			continue
		}
		assert.Less(t, s.Confidence, 0.5,
			"ambiguous class match for %q should report confidence < 0.5 (auto-create threshold), got %v with class_name %q",
			s.Name, s.Confidence, s.ClassName)
	}
}

// Helper
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// schemaProp mirrors the subset of JSON Schema shape produced by
// extractResponseSchema, enough to inspect the class_name enum constraint.
type schemaProp struct {
	Type       string                `json:"type"`
	Enum       []string              `json:"enum,omitempty"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
	Items      *schemaProp           `json:"items,omitempty"`
}

// TestExtractResponseSchemaClassNameEnum verifies that extractResponseSchema
// constrains class_name to an enum of the roster's actual class names, for
// both students[] and students[].candidates[], and that an empty roster
// falls back to a plain string instead of an unsatisfiable empty enum.
func TestExtractResponseSchemaClassNameEnum(t *testing.T) {
	classes := []ClassGroup{
		{Name: "Period 3", Students: []ClassStudent{{Name: "Maxence"}}},
		{Name: "Period 5", Students: []ClassStudent{{Name: "Amara"}}},
	}

	var schema schemaProp
	require.NoError(t, json.Unmarshal(extractResponseSchema(classes), &schema))

	studentItems := schema.Properties["students"].Items
	require.NotNil(t, studentItems)
	assert.ElementsMatch(t, []string{"Period 3", "Period 5"}, studentItems.Properties["class_name"].Enum)

	candidateItems := studentItems.Properties["candidates"].Items
	require.NotNil(t, candidateItems)
	assert.ElementsMatch(t, []string{"Period 3", "Period 5"}, candidateItems.Properties["class_name"].Enum)

	// Empty roster: no enum, plain string type.
	var emptySchema schemaProp
	require.NoError(t, json.Unmarshal(extractResponseSchema(nil), &emptySchema))
	emptyStudentItems := emptySchema.Properties["students"].Items
	require.NotNil(t, emptyStudentItems)
	emptyClassName := emptyStudentItems.Properties["class_name"]
	assert.Empty(t, emptyClassName.Enum)
	assert.Equal(t, "string", emptyClassName.Type)
}
