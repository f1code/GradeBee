package handler

import (
	"strings"
	"testing"
)

func TestBuildExtractionPrompt_ContainsClassConstraint(t *testing.T) {
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice"}, {Name: "Bob"}}},
		{Name: "Science", Students: []ClassStudent{{Name: "Charlie"}}},
	}

	prompt := buildExtractionPrompt(classes)

	// Should list all students with their classes.
	if !strings.Contains(prompt, "Alice (class Math 101)") {
		t.Error("prompt should list Alice with her class")
	}
	if !strings.Contains(prompt, "Charlie (class Science)") {
		t.Error("prompt should list Charlie with her class")
	}

	// Should contain a rule about strict class matching.
	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "must exactly match") || !strings.Contains(lower, "class name") {
		t.Error("prompt should contain a rule requiring class names to exactly match the roster")
	}
}
