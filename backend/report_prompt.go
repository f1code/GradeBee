// report_prompt.go builds GPT prompts for report card generation.
package handler

import (
	"fmt"
	"strings"
)

func buildReportPrompt(student, class, startDate, endDate string, notes []Note, examples []ReportExample, instructions, feedback string) string {
	var sb strings.Builder

	sb.WriteString("You are a report card writer for a school teacher.\n\n")

	// Style & layout guide
	sb.WriteString("## Style & Layout Guide\n")
	if len(examples) > 0 {
		sb.WriteString("The following are example report cards. Match their tone, voice, vocabulary,\nsection structure, and approximate length.\n\n")
		for i, ex := range examples {
			sb.WriteString(fmt.Sprintf("### Example %d: %s\n%s\n\n", i+1, ex.Name, ex.Content))
		}
	} else {
		sb.WriteString("Write a professional, warm report card narrative.\n\n")
	}

	// Additional instructions
	if instructions != "" {
		sb.WriteString("## Additional Instructions\n")
		sb.WriteString(instructions)
		sb.WriteString("\n\n")
	}

	// Student notes
	sb.WriteString("## Student Notes\n")
	sb.WriteString(fmt.Sprintf("Student: %s, Class: %s\n", student, class))
	sb.WriteString(fmt.Sprintf("Period: %s to %s\n\n", startDate, endDate))

	for _, n := range notes {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", n.Date, n.Summary))
	}
	sb.WriteString("\n")

	// Feedback on previous draft (for regeneration)
	if feedback != "" {
		sb.WriteString("## Teacher Feedback on Previous Draft\n")
		sb.WriteString(feedback)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Task\nWrite a report card narrative for this student based on the notes above.\n")
	sb.WriteString("Output the report as clean HTML (using <p>, <h3>, <ul>, <li> tags as appropriate).\n")
	sb.WriteString("Do not include <html>, <head>, or <body> wrapper tags — just the content HTML.\n")
	if len(examples) > 0 {
		sb.WriteString("Follow the style and layout of the examples provided.\n")
	}

	return sb.String()
}
