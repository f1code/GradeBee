// report_prompt.go builds GPT prompts for report card generation.
package handler

import (
	"fmt"
	"slices"
	"strings"
)

// BuildReportPrompt builds the GPT system prompt for report card generation.
// Exported for use by cmd/eval-cli.
func BuildReportPrompt(student, className string, notes []Note, reportInstructions, instructions, feedback string) string {
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
	for _, n := range notes {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", n.Date, n.Summary))
	}
	sb.WriteString("\n")

	// Feedback on previous draft (for regeneration)
	if feedback != "" {
		sb.WriteString(reportFeedbackHeader)
		sb.WriteString(feedback)
		sb.WriteString("\n\n")
	}

	sb.WriteString(reportTaskFooter)

	return sb.String()
}
