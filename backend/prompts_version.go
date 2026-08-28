// prompts_version.go provides deterministic hashes for prompt templates so
// every generated note and report row can be stamped with the prompt version
// that produced it.  This enables production quality drops to be correlated to
// specific prompt or model changes.
//
// # How it works
//
// Static template strings are defined here as package-level consts.  At init()
// time each string is hashed (SHA-256, first 12 hex chars) with a
// PromptVersionTag prefix so that non-template logic changes can be captured
// by manually bumping the tag.
//
// The builder functions in extract.go and report_prompt.go still live there and
// interpolate dynamic values (roster, notes, feedback) into the
// templates.  Hashing the static portion is a reasonable proxy: substantive
// changes almost always touch the static text.
package handler

import (
	"crypto/sha256"
	"encoding/hex"
)

// PromptVersionTag is bumped manually when non-template logic changes (e.g.
// branching behaviour inside builder functions that hashing the template alone
// would not catch).  Format: monotonic integer as string.
const PromptVersionTag = "4"

// --- Extraction prompt templates ---

// extractionPromptPrefix is the static preamble that opens every extraction
// prompt (before the per-class roster is interpolated).
const extractionPromptPrefix = `You are a teaching assistant analyzing a teacher's audio transcript about student observations.

Your task:
1. Identify which students are mentioned in the transcript
2. Match each mentioned name to the student roster below (handle phonetic/partial matches)
3. Extract the date if mentioned (format YYYY-MM-DD), otherwise leave empty
4. Write a summary per student capturing what the teacher said about them
   - Clean up speech artifacts (false starts, filler words, repetitions) into clear sentences
   - Preserve the teacher's voice, tone, and specific observations — do NOT add details or opinions not present in the transcript
   - Keep the teacher's vocabulary and perspective (first person if they used it)
   - Combine multiple mentions of the same student into a cohesive note

Student Roster:
`

// extractionPromptSuffix is the static rules block that closes every extraction
// prompt (after the per-class roster).
const extractionPromptSuffix = `
Rules:
- Match mentioned names against the roster even if pronunciation differs slightly
- Some roster entries include "(aka ...)" aliases — if a teacher uses an alias, match it to the canonical name and return the canonical name in the "name" field
- Set confidence 0.0-1.0 for each match. Use >= 0.7 for confident matches.
- If confidence < 0.7, include up to 3 closest roster matches in "candidates"
- "class_name" is REQUIRED to be one of the roster's real class names — you cannot leave it blank or invent one. If a student's name exists in more than one class and the transcript does not make clear which class is meant, you must still pick one class_name, but set confidence below 0.5 for that entry so it is not auto-created against a guessed class, and list the other plausible (name, class_name) pairs in "candidates". Only report confidence >= 0.5 for a class_name you are actually sure of.
- A student is "individually mentioned" ONLY if the teacher uses their name (or a recognizable nickname/variant of their name). Generic group references like "everyone", "all students", "the class" do NOT count as individual mentions.
- Do NOT create entries for students who are never individually mentioned by name. If a student is only covered by group-level observations (e.g. "the class was loud") but never called out by name, they must NOT appear in the output.
- For students who ARE individually mentioned by name, their quoted_text MUST include BOTH their individual observations AND any group-level observations from the same transcript/class. This applies to EVERY individually-mentioned student in that class, regardless of where the group observation appears in the transcript (beginning, middle, or end) or which students were named immediately before/after it. Do not attach group observations only to the students mentioned closest to them — propagate them to all named students in the class.
  Example: Transcript says "Alice did great. Bob was quiet. The whole class struggled with fractions." → BOTH Alice's and Bob's quoted_text must include the fractions observation, not just Bob's.
- If the transcript contains group references like "everyone", "all students", or "the class", apply those observations only to students in the class being discussed, not to ALL classes. Use context clues (class name mentions, prior student mentions) to determine which class is meant.
- For multi-student transcripts, produce a separate entry per student with relevant passages
- If a mentioned student cannot be matched to any roster entry, do not include them in the output
- If no students are clearly mentioned, return an empty students array
- The "class_name" field for each student MUST exactly match one of the class names from the roster above. Do not invent or abbreviate class names — if you are unsure which class a student belongs to, see the confidence/candidates rule above.
- IMPORTANT: Clean up speech into readable sentences, but do NOT invent observations or editorialize. Stay faithful to what the teacher actually said.
`

// --- Report prompt templates ---

// reportPromptBase is the static opening of every report prompt.
const reportPromptBase = "You are a report card writer for a school teacher.\n" +
	"The student notes are the sole source of facts for the report.\n" +
	"Every observation, data field, and mark must come from the notes — not from any examples.\n\n"

// reportSpecHeader is prefixed before the Level's mandatory Report
// Specification — the required structure, sections, and content for the
// report.
const reportSpecHeader = "## Report Specification (defines the report's required structure, sections, and content — follow it)\n\n"

// reportInstructionsHeader prefixes the optional ad-hoc-instructions block.
const reportInstructionsHeader = "## Teacher's Instructions for This Report — override the Report Specification where they conflict\n\n"

// reportNotesHeader prefixes the student notes section.
const reportNotesHeader = "## Student Notes (source of truth — all report content must derive from these)\n"

// reportFeedbackHeader prefixes the feedback-on-previous-draft block.
const reportFeedbackHeader = "## Teacher Feedback on Previous Draft\n"

// reportTaskFooter is the static closing instructions in every report prompt.
const reportTaskFooter = "## Task\nWrite a report card narrative for this student based on the notes above.\n" +
	"Output the report as clean HTML (using <p>, <h3>, <ul>, <li> tags as appropriate).\n" +
	"Do not include <html>, <head>, or <body> wrapper tags — just the content HTML.\n" +
	"Only include structured Data fields (Absences, Marks, Frequency of use, etc.) if those\n" +
	"values are explicitly present in the notes.\n" +
	"Every statement in the report must be traceable to the notes. Do not invent observations.\n"

// --- Computed hashes (populated at init) ---

// ExtractionPromptHash is the short hash of the extraction prompt templates.
// Stamped on every auto-extracted note row.
var ExtractionPromptHash string

// ReportPromptHash is the short hash of the report-generation prompt templates.
// Stamped on every generated report row.
var ReportPromptHash string

func init() {
	// The extraction hash covers both the prefix and suffix (the roster is
	// dynamic, so we use a sentinel to represent it).
	ExtractionPromptHash = hashPrompt(extractionPromptPrefix + "<<<roster>>>" + extractionPromptSuffix)

	// The report hash covers all static fragments concatenated with separators.
	// Dynamic parts (student name, notes, examples, feedback) are represented by
	// sentinels so the hash captures the structural template, not the data.
	reportTemplate := reportPromptBase +
		reportSpecHeader + "<<<reportInstructions>>>" +
		reportInstructionsHeader + "<<<instructions>>>" +
		reportNotesHeader + "<<<notes>>>" +
		reportFeedbackHeader + "<<<feedback>>>" +
		reportTaskFooter
	ReportPromptHash = hashPrompt(reportTemplate)
}

// hashPrompt returns the first 12 hex characters of SHA-256(PromptVersionTag + ":" + s).
func hashPrompt(s string) string {
	h := sha256.Sum256([]byte(PromptVersionTag + ":" + s))
	return hex.EncodeToString(h[:])[:12]
}
