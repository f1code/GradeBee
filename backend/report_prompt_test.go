package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildReportPrompt_SpecSectionAboveStyleGuide verifies the Report
// Specification appears above the Style & Layout Guide — the whole point of
// the instructions channel is that it outranks it and any examples.
func TestBuildReportPrompt_SpecSectionAboveStyleGuide(t *testing.T) {
	examples := []ReportExample{{Name: "ex1", Content: "example body"}}
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, examples, "Write three sections.", "", "")

	specIdx := strings.Index(prompt, "Report Specification")
	styleIdx := strings.Index(prompt, "Style & Layout Guide")
	require.GreaterOrEqual(t, specIdx, 0, "Report Specification header must be present")
	require.GreaterOrEqual(t, styleIdx, 0, "Style & Layout Guide header must be present")
	assert.Less(t, specIdx, styleIdx, "Report Specification must appear before the Style & Layout Guide")
	assert.Contains(t, prompt, "Write three sections.")
}

// TestBuildReportPrompt_AdHocInstructionsOverrideSpec verifies the ad-hoc
// instructions header states it overrides the Level's Report Specification.
func TestBuildReportPrompt_AdHocInstructionsOverrideSpec(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, nil, "spec text", "be extra concise", "")
	assert.Contains(t, prompt, "override the Report Specification")
	assert.Contains(t, prompt, "be extra concise")
}

// TestBuildReportPrompt_NoDanglingStyleHeaderWithoutExamples verifies the
// Style & Layout Guide header itself is absent when no examples are
// provided — the header must not appear detached from its body.
func TestBuildReportPrompt_NoDanglingStyleHeaderWithoutExamples(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "3A", nil, nil, "spec text", "", "")
	assert.NotContains(t, prompt, "Style & Layout Guide")
}
