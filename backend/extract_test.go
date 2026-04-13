package handler

import (
	"context"
	"strings"
	"testing"
)

// TestExtractPreservesTeacherVoice verifies that extracted QuotedText preserves
// the teacher's original language and emotion, not rewritten summaries.
func TestExtractPreservesTeacherVoice(t *testing.T) {
	extractor, err := newGPTExtractor()
	if err != nil {
		t.Skipf("OPENAI_API_KEY not set: %v", err)
	}

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
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Students) != 2 {
		t.Fatalf("Expected 2 students, got %d", len(result.Students))
	}

	// Find Maxence
	var maxence *MatchedStudent
	for i := range result.Students {
		if result.Students[i].Name == "Maxence" {
			maxence = &result.Students[i]
			break
		}
	}
	if maxence == nil {
		t.Fatal("Maxence not found in extraction")
	}

	// Verify QuotedText contains original phrasing, not formal rewrite
	if maxence.QuotedText == "" {
		t.Error("QuotedText is empty")
	}

	// The quoted text should contain evidence of teacher's original voice
	// (not formal language like "had a very difficult day")
	if !contains(maxence.QuotedText, "impossibly bad") {
		t.Errorf("QuotedText does not preserve original phrasing. Got: %s", maxence.QuotedText)
	}
}

// TestExtractGroupObservations verifies that group-level observations
// are included for individually mentioned students but do NOT create
// entries for unmentioned students.
func TestExtractGroupObservations(t *testing.T) {
	extractor, err := newGPTExtractor()
	if err != nil {
		t.Skipf("OPENAI_API_KEY not set: %v", err)
	}

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
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Only Tommy should be extracted — Lisa is not individually mentioned
	if len(result.Students) != 1 {
		names := make([]string, len(result.Students))
		for i, s := range result.Students {
			names[i] = s.Name
		}
		t.Fatalf("Expected 1 student (Tommy only), got %d: %v", len(result.Students), names)
	}

	tommy := result.Students[0]
	if tommy.Name != "Tommy" {
		t.Fatalf("Expected Tommy, got %s", tommy.Name)
	}

	// Tommy's QuotedText should include both his individual mention and the group observation
	if !contains(tommy.QuotedText, "organize") {
		t.Errorf("Tommy QuotedText missing individual observation. Got: %s", tommy.QuotedText)
	}
	if !contains(tommy.QuotedText, "too loud") && !contains(tommy.QuotedText, "unfocused") && !contains(tommy.QuotedText, "talking over") {
		t.Errorf("Tommy QuotedText missing group observation. Got: %s", tommy.QuotedText)
	}
}

// TestExtractGroupObservationsMultiClass verifies that group-level observations
// are scoped to the class being discussed, not applied across all classes.
func TestExtractGroupObservationsMultiClass(t *testing.T) {
	extractor, err := newGPTExtractor()
	if err != nil {
		t.Skipf("OPENAI_API_KEY not set: %v", err)
	}

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
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Only Tommy and Sarah should be extracted (individually mentioned).
	// Lisa and Jake are not mentioned by name.
	nameSet := make(map[string]MatchedStudent)
	for _, s := range result.Students {
		nameSet[s.Name] = s
	}

	if _, ok := nameSet["Tommy"]; !ok {
		t.Error("Tommy should be extracted (individually mentioned)")
	}
	if _, ok := nameSet["Sarah"]; !ok {
		t.Error("Sarah should be extracted (individually mentioned)")
	}
	if _, ok := nameSet["Lisa"]; ok {
		t.Error("Lisa should NOT be extracted (not individually mentioned)")
	}
	if _, ok := nameSet["Jake"]; ok {
		t.Error("Jake should NOT be extracted (not individually mentioned)")
	}

	// Tommy should have the group observation about the class being loud
	tommy := nameSet["Tommy"]
	if !contains(tommy.QuotedText, "loud") {
		t.Errorf("Tommy should include Period 1 group observation about loudness. Got: %s", tommy.QuotedText)
	}

	// Sarah should NOT have the "loud" group observation — that was about Period 1
	sarah := nameSet["Sarah"]
	if contains(sarah.QuotedText, "loud") {
		t.Errorf("Sarah should NOT have Period 1's group observation. Got: %s", sarah.QuotedText)
	}
}

// Helper
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
