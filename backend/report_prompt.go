// report_prompt.go builds GPT prompts for report card generation.
package handler

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// ReportRevision is what a regenerate adds to the prompt: the draft, the
// feedback that wrote it and the feedback to apply now.
type ReportRevision struct {
	Draft string
	// DraftCreatedAt tags notes edited after it "(new)"; RFC 3339, empty or
	// unparsable tags none.
	DraftCreatedAt string
	// EarlierFeedback is the feedback of the rounds that wrote Draft, oldest first.
	EarlierFeedback []string
	Feedback        string
}

// BuildReportPrompt builds the GPT system prompt for report card generation;
// rev is nil for a first draft. Exported for use by cmd/eval-cli.
func BuildReportPrompt(student, className string, notes []Note, reportInstructions, instructions string, rev *ReportRevision) string {
	var sb strings.Builder

	sb.WriteString(reportPromptBase)

	// Report Specification (Level-level, mandatory)
	sb.WriteString(reportSpecHeader)
	sb.WriteString(reportInstructions)
	sb.WriteString("\n\n")

	// Additional instructions
	if instructions != "" {
		sb.WriteString(reportInstructionsHeader)
		sb.WriteString(instructions)
		sb.WriteString("\n\n")
	}

	// Student notes
	sb.WriteString(reportNotesHeader)
	sb.WriteString(fmt.Sprintf("Student: %s, Class: %s\n\n", student, className))
	sb.WriteString(reportNotesFiling)

	// Oldest first, so the newest notes sit next to the task, where the model
	// weighs them most. Sorted here, not only in SQL: eval fixtures bypass the repo.
	notes = slices.Clone(notes)
	slices.SortStableFunc(notes, func(a, b Note) int { return strings.Compare(a.Date, b.Date) })
	// Parsed, not compared as strings: rows written with and without
	// milliseconds coexist, and "...:00Z" sorts after "...:00.500Z".
	var draftAt time.Time
	tagNew := false
	if rev != nil {
		if t, err := time.Parse(time.RFC3339Nano, rev.DraftCreatedAt); err == nil {
			draftAt, tagNew = t, true
		}
	}
	for _, n := range notes {
		tag := ""
		if updated, err := time.Parse(time.RFC3339Nano, n.UpdatedAt); tagNew && err == nil && updated.After(draftAt) {
			tag = reportNewNoteTag
		}
		sb.WriteString(fmt.Sprintf("- %s%s: %s\n", n.Date, tag, n.Summary))
	}
	sb.WriteString("\n")

	if rev != nil {
		writeRevision(&sb, rev)
	}

	sb.WriteString(reportTaskFooter)

	return sb.String()
}

func writeRevision(sb *strings.Builder, rev *ReportRevision) {
	sb.WriteString(reportPrevDraftHeader)
	sb.WriteString(rev.Draft)
	sb.WriteString("\n\n")

	// Rounds before this one, so their removals hold against the notes (#214).
	if len(rev.EarlierFeedback) > 0 {
		sb.WriteString(reportEarlierFeedbackHeader)
		for i, fb := range rev.EarlierFeedback {
			fmt.Fprintf(sb, reportEarlierFeedbackRound, i+1, fb)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(reportFeedbackHeader)
	sb.WriteString(rev.Feedback)
	sb.WriteString("\n\n")
}
