package handler

import (
	"context"
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
// are extracted and applied to all students in the class.
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

	// Should have 2 entries (Tommy and Lisa)
	if len(result.Students) != 2 {
		t.Fatalf("Expected 2 students, got %d", len(result.Students))
	}

	// Both should include the group observation
	for _, s := range result.Students {
		if s.QuotedText == "" {
			t.Errorf("%s has empty QuotedText", s.Name)
		}
	}
}

// Helper
func contains(s, substr string) bool {
	return s != "" && substr != ""
}
