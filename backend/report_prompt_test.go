package handler

import (
	"strings"
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

func TestBuildReportPrompt_NotesOldestFirst(t *testing.T) {
	notes := []Note{{Date: "2026-11-05", Summary: "late"}, {Date: "2026-10-01", Summary: "early"}}
	prompt := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", "")
	assert.Less(t, strings.Index(prompt, "early"), strings.Index(prompt, "late"))
	assert.Equal(t, "late", notes[0].Summary, "caller's slice untouched")
}

// Two spellings of one child read as two children (task 186), so the filing
// statement sits between the student line and the notes.
func TestBuildReportPrompt_NotesFiledToStudent(t *testing.T) {
	notes := []Note{{Date: "2026-09-11", Summary: "Alise repeated after me."}}
	prompt := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", "")
	assert.Contains(t, prompt, "Student: Alice, Class: Grade 3A\n\n"+reportNotesFiling+"- 2026-09-11")
	assert.Contains(t, reportNotesFiling, "however spelled, is this student")
}

// #188: reports reused facts across sections and invented before/after stories.
func TestBuildReportPrompt_FactOnceAndDatedChange(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", "")
	assert.Contains(t, prompt, "Use each fact from the notes in one section only.")
	assert.Contains(t, prompt, "Describe a change over time only when notes on different dates show it or a note states it; "+
		"what the latest note says is how the student is now.")
}
