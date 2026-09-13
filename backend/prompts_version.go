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
//
// # Extraction also hashes its schemas
//
// Under structured output, behaviour comes from the prompt and the schema
// together. #127 is the worked example: adding "" to pass 1's enum made the
// model decline a recording it cannot place, with the prompt text unchanged
// byte for byte. Hashing text alone gave both contracts one value, so a Sentry
// readout showed one bucket spanning two behaviours.
//
// So the extraction hash also covers the bytes of classPickSchema and
// passageSchema, built with sentinelClasses. Schema shape moves the hash; a
// teacher's class and student names cannot. The bytes go in raw, so a property
// reorder moves the hash too — that order is the model's generation order (see
// passageSchema).
//
// Reports go through ChatText with no schema, so ReportPromptHash covers the
// templates alone.
package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// PromptVersionTag is bumped manually when non-template logic changes (e.g.
// branching behaviour inside builder functions that hashing the template alone
// would not catch).  Format: monotonic integer as string.
// 6: #127 added "" to pass 1's enum. Only the schema changed, and back then
// ExtractionPromptHash could not move on its own, so the bump carried it. Since
// #129 the schema bytes are part of the extraction hash input: a schema shape
// change moves the hash unaided and needs no bump.
//
// The tag is shared, so a bump re-stamps ReportPromptHash too even when no
// report template moved. Report rows written before and after this bump carry
// different hashes for identical prompts; nothing is keyed on it beyond the
// stamp, so this only means a quality readout grouped by hash shows one prompt
// in two buckets.
const PromptVersionTag = "6"

// --- Extraction prompt templates ---
//
// Extraction is two calls (#125). Pass 1 names the class from the class list
// alone; pass 2 sees only that class's roster and cuts the transcript into
// passages. Both texts are byte-identical to the arm that was measured in
// research/2026-09-05-123-summaries-vs-spans (probe.go consts tPass1,
// tPass1Suffix, and vBase rendered with vNoElim — arm V1). The V1 rules took
// the roster phantom from 8/10 to 0/10 at no recall cost, and that result is
// pinned to this exact wording. Re-wrap a bullet and the measurement no longer
// describes what ships.

// classPickPrompt opens pass 1: the class list, and nothing about the children.
const classPickPrompt = `You are reading a teacher's spoken notes about their students. Say which class the
recording is about. The transcript usually opens with a spoken header — level name, weekday,
time. Match it to one of:
`

// classPickPromptSuffix closes pass 1, after the class names.
//
// It tells the model to return "" when no class is identifiable, and since #127
// classPickSchema's enum carries "" so the instruction can be obeyed. Measured
// live on mistral-medium-2508 against the two-class multi_class fixture: the
// no-"" enum pins 3/3, the same prompt with "" in the enum declines. The text
// did not change between those two measurements and does not change here.
const classPickPromptSuffix = `
If the header is missing, or does not clearly identify exactly one of the classes listed,
return "" — an empty string — rather than guessing.
`

// passagePromptPrefix is the whole of pass 2's system prompt except the roster,
// which BuildPassagePrompt appends.
//
// #142 added the "absent" kind. Two edits, both measured on
// mistral-medium-2508, 5 samples per arm:
//
// The bullet carries no scope exclusions. Late arrival and early departure come
// back "child" 20/20 with or without a sentence excluding them. An absence on
// another day does not — 10/10 "absent" with no exclusion, 10/10 "child" with
// one reading `An absence on another day ("Margot was out all last week") is a
// "child" passage, not this one`, whose example is load-bearing (without it,
// 3/5, and the other child's passage lost). It is left out anyway: five months
// of notes (260 rows, 646 children, 140 classes) mention no absence at all, on
// this day or another, and when it is wrong the child still gets their note —
// only #148's fan-out skips them, for one recording. Paste the sentence back
// if a teacher ever dictates last week.
//
// "Children on the list who are never named were absent or not discussed
// today" lost "absent or", which the kind makes false: silence now reads as
// presence. Trimmed, not deleted — deleting it whole took date_drill 4/4 → 1/4
// (the example date stops reaching the group summary), and the kind alone
// holds 4/4, so that row rests on the sentence, not on the kind.
//
// #151 added the sentence on a shared remark that opens its own sentence after
// the names ("… Bruno had to be talked to. They both worked well"). Without it
// the model returns that sentence "group" 5/5, and since #143 group text
// reaches the whole roster, so a child never named gets it. Measured on
// mistral-medium-2508 against shared_clause and date_drill:
//   - no edit: shared_clause 0/5, date_drill 15/15
//   - this sentence: shared_clause 10/10, date_drill 39/40
//   - plus a group-bullet line excluding "they both": shared_clause 5/5,
//     date_drill 12/15
//   - plus that line and an example transcript inside this sentence:
//     date_drill 0/5
// In the 0/5 arm the model cut the example date in date_drill into its own
// "none" passage, so no child got it. Raw output of the 12/15 arm was not
// inspected, and the example alone was not measured. The
// sentence demands the child's name in "spoken_labels" because "they" and
// "both" are on labelStopList, and guardPassages would demote the copy.
const passagePromptPrefix = `You are extracting a teacher's spoken notes about the children in one class.

The notes arrive as a transcript, in the order the teacher spoke them. The children in this
class are listed below. Names in the transcript are speech-to-text and often misspelt or
mangled; match a spoken name to the listed child it most plausibly is.

Return "observations": the transcript cut into contiguous passages, in order, one passage
per owner, together covering the whole transcript.

Each passage has:
- "kind":
  - "child" — the teacher is talking about one individual child and speaks a name for them.
  - "absent" — the teacher says a named child was not there today ("Théo wasn't in today").
  - "unknown" — the teacher is talking about one individual child but no name is spoken for
    them in this passage or the passage it continues: only a pronoun, or a name that
    matches nobody listed. Do not guess. The teacher will assign it.
  - "group" — a statement about the class as a whole, using a collective referent
    ("everyone", "all the kids", "the class", "they" meaning the whole group). A statement
    that names one child, or describes only one child, is NEVER "group", however it is
    joined to the rest of the sentence.
  - "none" — not an observation about children: the date, the class header, a greeting,
    vocabulary the children are being taught, thinking aloud that describes no child and
    no class.
- "spoken_labels": for a "child" or "absent" passage, the name the teacher speaks for it,
  verbatim as spoken, uncorrected. Empty list for "unknown", "group" and "none".
- "student": for a "child" or "absent" passage, the listed child's name exactly as listed
  below, or "" when no listed child fits. "" for every other kind.
- "summary": the observations in that passage, rewritten as clear sentences.

Rules:
- A child passage runs from where the teacher starts talking about that child to where the
  teacher moves on. It usually opens with the child's name and then continues in pronouns;
  every pronoun sentence after that name belongs to that child until the next child is
  named. Do NOT open a new passage for a pronoun the teacher is still using for the same
  child.
- When the teacher makes the same observation about several named children at once
  ("Zachariah and Anaya did very well", "they both worked well"), return that passage once
  PER CHILD: the same summary repeated, each copy with its own "student". Never fold two
  named children into one passage. This holds when the shared remark opens its own sentence
  after the names: each copy's "spoken_labels" holds the name the teacher spoke for that
  child, never "they" or "both". If the observations differ between the children, they
  are separate passages with different summaries. A statement about the class as a whole
  is still one "group" passage, not one per child.
- "student" is set ONLY when the passage's own words, or the passage it continues, speak
  a name for the child — a name that appears in "spoken_labels". A passage that refers to
  the child only by a pronoun ("she", "he") has NO student: it is "unknown", even when
  exactly one listed child has not been mentioned yet. Children on the list who are never
  named were not discussed today. Never assign a passage to a child by
  elimination, by roster order, or because they are the only one left.
- The list of children exists to spell spoken names correctly, not to decide who is
  present.
- The summary uses ONLY the words in its own passage. Never bring in an observation from
  another passage, even an adjacent one.
- The summary keeps the teacher's voice: their vocabulary, their tone, first person if they
  used it. Clean up false starts, filler words and repetitions. Do NOT soften, formalise,
  add or editorialise.
- "none" never takes an observation with it. The header passage ends with the header
  itself: the first sentence that describes the children opens a new passage, even when it
  is spoken in the same breath.

The children in this class:
`

// --- Report prompt templates ---

// reportPromptBase is the static opening of every report prompt.
const reportPromptBase = "You are a report card writer for a school teacher.\n" +
	"The student notes are the sole source of facts for the report.\n" +
	"Every observation, data field, and mark must come from the notes — " +
	"not from any examples.\n\n"

// reportSpecHeader is prefixed before the Level's mandatory Report
// Specification — the required structure, sections, and content for the
// report.
const reportSpecHeader = "## Report Specification (defines the report's required " +
	"structure, sections, and content — follow it)\n\n"

// reportInstructionsHeader prefixes the optional ad-hoc-instructions block.
const reportInstructionsHeader = "## Teacher's Instructions for This Report — " +
	"override the Report Specification where they conflict\n\n"

// reportNotesHeader prefixes the student notes section.
const reportNotesHeader = "## Student Notes (source of truth — all report content " +
	"must derive from these)\n"

// reportFeedbackHeader prefixes the feedback-on-previous-draft block.
const reportFeedbackHeader = "## Teacher Feedback on Previous Draft\n"

// reportTaskFooter is the static closing instructions in every report prompt.
const reportTaskFooter = "## Task\n" +
	"Write a report card narrative for this student based on the notes above.\n" +
	"Output the report as clean HTML (using <p>, <h3>, <ul>, <li> tags as appropriate).\n" +
	"Do not include <html>, <head>, or <body> wrapper tags — just the content HTML.\n" +
	"Only include structured Data fields (Absences, Marks, Frequency of use, etc.) if those\n" +
	"values are explicitly present in the notes.\n" +
	"Every statement in the report must be traceable to the notes. " +
	"Do not invent observations.\n"

// --- Computed hashes (populated at init) ---

// ExtractionPromptHash is the short hash of the extraction prompt templates.
// Stamped on every auto-extracted note row.
var ExtractionPromptHash string

// ReportPromptHash is the short hash of the report-generation prompt templates.
// Stamped on every generated report row.
var ReportPromptHash string

func init() {
	// The extraction hash covers both passes, in the order they run, with
	// sentinels for the dynamic lists. One hash, not two: a note is produced by
	// the pair, so a stamp naming only one of them would not identify what
	// wrote it.
	ExtractionPromptHash = hashPrompt(extractionHashInput(
		classPickSchema(sentinelClasses), passageSchema(sentinelClasses[0])))

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

// sentinelClasses is the fixed roster both extraction schemas are built with
// for hashing. A teacher's real classes and children must never reach the hash,
// or every teacher would stamp their notes with a different prompt version.
//
// Two classes, two children in the first, one of them with an alias: enough
// shape that a schema change has somewhere to show up. At one class and one
// child, widening the student enum to include aliases emits identical bytes —
// BuildPassagePrompt already reads them, so that is the cheapest next change of
// exactly #127's kind — and so does anything that depends on how many classes
// or children there are. Either would leave the hash still, which is the silent
// miss this hash exists to remove.
//
// A reordering still slips through: these names are declared in sorted order,
// so a sort added to either enum emits identical bytes. Swap two of them if
// that case ever needs covering.
//
// The names carry no angle brackets, unlike the prompt separators, because
// encoding/json escapes those to \u003c.
var sentinelClasses = []ClassGroup{
	{
		Name: "SENTINEL_CLASS_A",
		Students: []ClassStudent{
			{Name: "SENTINEL_STUDENT_A", Aliases: []string{"SENTINEL_ALIAS_A"}},
			{Name: "SENTINEL_STUDENT_B"},
		},
	},
	{
		Name:     "SENTINEL_CLASS_B",
		Students: []ClassStudent{{Name: "SENTINEL_STUDENT_C"}},
	},
}

// extractionHashInput assembles what the extraction hash is taken over: both
// passes' text in the order they run, each followed by its schema.
//
// The schemas arrive as parameters so a test can hash a mutated schema against
// unchanged prompt text and show the hash moves.
func extractionHashInput(classPick, passage json.RawMessage) string {
	return classPickPrompt + "<<<classes>>>" + classPickPromptSuffix +
		"<<<pass1schema>>>" + string(classPick) +
		"<<<pass2>>>" + passagePromptPrefix + "<<<roster>>>" +
		"<<<pass2schema>>>" + string(passage)
}

// hashPrompt returns the first 12 hex characters of SHA-256(PromptVersionTag + ":" + s).
func hashPrompt(s string) string {
	h := sha256.Sum256([]byte(PromptVersionTag + ":" + s))
	return hex.EncodeToString(h[:])[:12]
}
