package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildReportPrompt_AdHocInstructionsOverrideSpec verifies the ad-hoc
// instructions header states it overrides the Level's Report Specification.
func TestBuildReportPrompt_AdHocInstructionsOverrideSpec(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "be extra concise", nil)
	assert.Contains(t, prompt, "override the Report Specification")
	assert.Contains(t, prompt, "be extra concise")
}

// TestBuildReportPrompt_InstructionsSectionOnlyWhenGiven verifies the
// Teacher's Instructions header is emitted only alongside a body — an empty
// instruction set must not leave a dangling header. The positive arm proves
// the header string is the one the function really writes.
func TestBuildReportPrompt_InstructionsSectionOnlyWhenGiven(t *testing.T) {
	without := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", nil)
	assert.NotContains(t, without, reportInstructionsHeader)

	with := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "be extra concise", nil)
	assert.Contains(t, with, reportInstructionsHeader+"be extra concise")
}

// TestBuildReportPrompt_NotesRemainSoleSourceOfFacts verifies the base
// framing sentence survives the reframe.
func TestBuildReportPrompt_NotesRemainSoleSourceOfFacts(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", nil)
	assert.Contains(t, prompt, "student notes are the sole source of facts")
}

// TestBuildReportPrompt_RevisionInOrder: a first draft has no revision
// sections; a regenerate puts draft, earlier rounds oldest first, then the
// current feedback between the notes and the task.
func TestBuildReportPrompt_RevisionInOrder(t *testing.T) {
	without := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", nil)
	assert.NotContains(t, without, reportPrevDraftHeader)
	assert.NotContains(t, without, reportEarlierFeedbackHeader)
	assert.NotContains(t, without, reportFeedbackHeader)

	notes := []Note{{Date: "2026-10-01", Summary: "zebra-note"}}
	with := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "be concise", &ReportRevision{
		Draft:           "<p>draft</p>",
		EarlierFeedback: []string{"no cushions", "warmer"},
		Feedback:        "make it shorter",
	})
	assert.Contains(t, with, reportPrevDraftHeader+"<p>draft</p>")
	assert.Contains(t, with, reportFeedbackHeader+"make it shorter")
	order := []string{reportSpecHeader, reportInstructionsHeader, "zebra-note", reportPrevDraftHeader,
		reportEarlierFeedbackHeader, "Round 1: no cushions\n", "Round 2: warmer\n", reportFeedbackHeader, reportTaskFooter}
	for i := 1; i < len(order); i++ {
		require.NotEqual(t, -1, strings.Index(with, order[i]), "%q missing", order[i])
		assert.Less(t, strings.Index(with, order[i-1]), strings.Index(with, order[i]), "%q before %q", order[i-1], order[i])
	}
}

// TestBuildReportPrompt_NewNotesTagged: only a note edited after the draft is
// "(new)"; one edited the same instant is not. Mixed formats compare by time:
// as strings, ".500Z" would sort before the draft's "Z".
func TestBuildReportPrompt_NewNotesTagged(t *testing.T) {
	const draftAt = "2026-10-12T09:00:00Z"
	notes := []Note{
		{Date: "2026-10-01", Summary: "before", UpdatedAt: "2026-10-11T09:00:00.000Z"},
		{Date: "2026-10-02", Summary: "same", UpdatedAt: "2026-10-12T09:00:00.000Z"},
		{Date: "2026-10-03", Summary: "after", UpdatedAt: "2026-10-12T09:00:00.500Z"},
	}
	prompt := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", &ReportRevision{
		Draft: "<p>draft</p>", DraftCreatedAt: draftAt, Feedback: "warmer",
	})
	assert.Contains(t, prompt, "- 2026-10-01: before\n")
	assert.Contains(t, prompt, "- 2026-10-02: same\n")
	assert.Contains(t, prompt, "- 2026-10-03"+reportNewNoteTag+": after\n")

	first := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", nil)
	assert.Contains(t, first, "- 2026-10-03: after\n", "a first draft tags nothing")
}

func TestBuildReportPrompt_NotesOldestFirst(t *testing.T) {
	notes := []Note{{Date: "2026-11-05", Summary: "late"}, {Date: "2026-10-01", Summary: "early"}}
	prompt := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", nil)
	assert.Less(t, strings.Index(prompt, "early"), strings.Index(prompt, "late"))
	assert.Equal(t, "late", notes[0].Summary, "caller's slice untouched")
}

// Two spellings of one child read as two children (task 186), so the filing
// statement sits between the student line and the notes.
func TestBuildReportPrompt_NotesFiledToStudent(t *testing.T) {
	notes := []Note{{Date: "2026-09-11", Summary: "Alise repeated after me."}}
	prompt := BuildReportPrompt("Alice", "Grade 3A", notes, "spec text", "", nil)
	assert.Contains(t, prompt, "Student: Alice, Class: Grade 3A\n\n"+reportNotesFiling+"- 2026-09-11")
	assert.Contains(t, reportNotesFiling, "however spelled, is this student")
}

// #188: reports reused facts across sections and invented before/after stories.
func TestBuildReportPrompt_FactOnceAndDatedChange(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", nil)
	assert.Contains(t, prompt, "Use each fact from the notes in one section only.")
	assert.Contains(t, prompt, "Describe a change over time only when notes on different dates show it or a note states it; "+
		"what the latest note says is how the student is now.")
}

// #196: gpt-6-luna cited its source ("the notes also record…").
func TestBuildReportPrompt_NoSourceMention(t *testing.T) {
	prompt := BuildReportPrompt("Alice", "Grade 3A", nil, "spec text", "", nil)
	assert.Contains(t, prompt, "Write each fact as something observed in class; never mention the notes or records, "+
		"or that something was noted or recorded.")
}
