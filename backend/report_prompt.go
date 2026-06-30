// report_prompt.go builds GPT prompts for report card generation.
package handler

import (
	"fmt"
	"strings"
)

// BuildReportPrompt builds the GPT system prompt for report card generation.
// Exported for use by cmd/eval-cli.
func BuildReportPrompt(student, className string, notes []Note, examples []ReportExample, instructions, feedback string) string {
	var sb strings.Builder

	sb.WriteString(reportPromptBase)

	// Style & layout guide
	sb.WriteString(reportStyleHeader)
	if len(examples) > 0 {
		sb.WriteString(reportStyleWithExamples)
		for i, ex := range examples {
			sb.WriteString(fmt.Sprintf("### Example %d: %s\n%s\n\n", i+1, ex.Name, ex.Content))
		}
	} else {
		sb.WriteString(reportStyleNoExamples)
	}

	// Additional instructions
	if instructions != "" {
		sb.WriteString(reportInstructionsHeader)
		sb.WriteString(instructions)
		sb.WriteString("\n\n")
	}

	// Student notes
	sb.WriteString(reportNotesHeader)
	sb.WriteString(fmt.Sprintf("Student: %s, Class: %s\n\n", student, className))

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
	if len(examples) > 0 {
		sb.WriteString(reportTaskFooterWithExamples)
	}

	return sb.String()
}
