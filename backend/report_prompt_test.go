package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildReportPrompt_AdHocInstructionsOverrideSpec verifies the ad-hoc
// instructions header states it overrides the Level's Report Specification.
func TestBuildReportPrompt_AdHocInstructionsOverrideSpec(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "be extra concise", "")
	assert.Contains(t, prompt, "override the Report Specification")
	assert.Contains(t, prompt, "be extra concise")
}

// TestBuildReportPrompt_NoDanglingStyleHeaderWithoutExamples verifies the
// Style & Layout Guide header itself is absent when no examples are
// provided — the header must not appear detached from its body.
func TestBuildReportPrompt_NoDanglingStyleHeaderWithoutExamples(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.NotContains(t, prompt, "Style & Layout Guide")
}

// TestBuildReportPrompt_NotesRemainSoleSourceOfFacts verifies the base
// framing sentence survives the reframe.
func TestBuildReportPrompt_NotesRemainSoleSourceOfFacts(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.Contains(t, prompt, "student notes are the sole source of facts")
}

// TestBuildReportPrompt_NoStyleFallbackWithoutExamples verifies there's no
// styleless "write a professional narrative" fallback fragment left — the
// mandatory Report Specification replaces it.
func TestBuildReportPrompt_NoStyleFallbackWithoutExamples(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.NotContains(t, prompt, "Write a professional, warm report card narrative")
}
