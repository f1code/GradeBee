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

// TestBuildReportPrompt_InstructionsSectionOnlyWhenGiven verifies the
// Teacher's Instructions header is emitted only alongside a body — an empty
// instruction set must not leave a dangling header. The positive arm proves
// the header string is the one the function really writes.
func TestBuildReportPrompt_InstructionsSectionOnlyWhenGiven(t *testing.T) {
	without := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.NotContains(t, without, reportInstructionsHeader)

	with := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "be extra concise", "")
	assert.Contains(t, with, reportInstructionsHeader+"be extra concise")
}

// TestBuildReportPrompt_NotesRemainSoleSourceOfFacts verifies the base
// framing sentence survives the reframe.
func TestBuildReportPrompt_NotesRemainSoleSourceOfFacts(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.Contains(t, prompt, "student notes are the sole source of facts")
}

// TestBuildReportPrompt_FeedbackSectionOnlyWhenGiven verifies the previous-
// draft feedback header is emitted only on regeneration, when feedback is
// present, and carries the feedback text when it is.
func TestBuildReportPrompt_FeedbackSectionOnlyWhenGiven(t *testing.T) {
	without := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.NotContains(t, without, reportFeedbackHeader)

	with := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "make it shorter")
	assert.Contains(t, with, reportFeedbackHeader+"make it shorter")
}
