// llm_live_test.go holds every test that calls a real LLM provider. Each
// skips itself via requireLiveLLM when the active provider's API key is
// unset, so `make test` is safe without credentials and exercises them
// automatically when a key is present.
package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireLiveLLM skips the test if the active LLM provider's API key is unset.
// It returns the configured provider for live tests that need it.
func requireLiveLLM(t *testing.T) LLMProvider {
	t.Helper()
	p, err := LoadProvider()
	if err != nil {
		t.Skipf("LLM provider not configured: %v", err)
	}
	return p
}

// newTestLLMExtractor creates an LLM extractor, skipping if the active provider's API key is not set.
func newTestLLMExtractor(t *testing.T) Extractor {
	t.Helper()
	provider := requireLiveLLM(t)
	return newLLMExtractor(provider)
}

// contains is a readability helper for the live extraction assertions.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestLLM_SingleStudentCorrectClass(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Alice Johnson demonstrated excellent problem-solving skills on today's algebra quiz. She scored 95% and helped her classmates understand the quadratic formula.",
		Classes:    classes,
	})
	require.NoError(t, err)
	require.Len(t, result.Students, 1, "got %+v", result.Students)
	assert.Equal(t, "Alice Johnson", result.Students[0].Name)
	assert.Equal(t, "Math 101", result.Students[0].ClassName)
}

func TestLLM_MultiStudentDifferentClasses(t *testing.T) {
	ext := newTestLLMExtractor(t)
	// Bob appears in both rosters — the LLM must use transcript context to pick the right class.
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Bob Smith"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Today I observed two students. In Math 101, Bob Smith was very engaged during the fractions lesson and volunteered to solve problems on the board. In Science 202, Diana Lee conducted her chemistry experiment carefully and wrote detailed lab notes.",
		Classes:    classes,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Students), 2, "got %+v", result.Students)

	found := map[string]string{}
	for _, s := range result.Students {
		found[s.Name] = s.ClassName
	}
	assert.Equal(t, "Math 101", found["Bob Smith"])
	assert.Equal(t, "Science 202", found["Diana Lee"])
}

func TestLLM_UnknownClassSkipped(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Report card for Tommy Wilson, Art 303. Tommy shows great creativity in his paintings and participates actively in class discussions about art history.",
		Classes:    classes,
	})
	require.NoError(t, err)

	// Tommy Wilson is not in any roster class. The extractor should return no students
	// (or possibly empty results). It must NOT invent a class name.
	validClasses := map[string]bool{"Math 101": true, "Science 202": true}
	for _, s := range result.Students {
		assert.True(t, validClasses[s.ClassName], "student %q assigned to invalid class %q", s.Name, s.ClassName)
	}
}

func TestLLM_PartialNameMatch(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "English 101", Students: []ClassStudent{{Name: "Alexander Hamilton"}, {Name: "Katherine Bennet"}}},
		{Name: "History 201", Students: []ClassStudent{{Name: "Theodore Roosevelt"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Alex Hamilton wrote an outstanding essay on democracy today. His arguments were well-structured and his writing has improved significantly this semester.",
		Classes:    classes,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Students), 1)

	var found bool
	for _, s := range result.Students {
		if s.Name == "Alexander Hamilton" {
			found = true
			assert.Equal(t, "English 101", s.ClassName)
			break
		}
	}
	assert.True(t, found, "Alexander Hamilton not found in results: %+v", result.Students)
}

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
