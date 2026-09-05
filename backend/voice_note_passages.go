// voice_note_passages.go folds the passages extraction returned into the notes
// a recording produces and the passage list its done card carries.
//
// Pure: no repo, no model, no clock. The pipeline (voice_note_process.go) does
// the I/O around it, and the rules live here because they are the whole of
// what a recording means — which stretch of speech reaches which child, and
// which reaches nobody.
package handler

// assembledNote is one child's note from one recording: everything the teacher
// said about them, in the order they said it.
type assembledNote struct {
	Name string
	// Summary is the child's passages joined, then the recording's class-wide
	// passages. Blank line between them: they are separate stretches of speech,
	// and running them together invents a sentence the teacher never said.
	Summary string
	// Passages is how many stretches of the recording reached this child,
	// group ones included. Telemetry only — it names how much of a recording a
	// note came from without naming the child (docs/adr/0003).
	Passages int
}

// assemblePassages returns one note per child the recording reached, and the
// passages the done card gets.
//
// The rules, in the order they matter:
//
//   - child with a roster student → that child's note. Several passages about
//     one child join in order; the model returns a shared observation once per
//     child, so a pair reaches both of them.
//   - child with no student → nobody. Its labels stay on the passage, which is
//     what the class picker re-resolves when the recording was read against the
//     wrong roster.
//   - unknown → nobody, and no labels to re-resolve: the recording never said
//     who this was.
//   - group → every child this recording already reached, and nobody else. A
//     child who was absent gets no note from "everyone did well", and a group
//     passage in a recording that named no child reaches no note at all.
//   - none → dropped, and not on the card. A recording holding nothing but a
//     spoken header yields no passages, so it reads as nobody named instead of
//     offering the class picker over a passage there is nothing to pick for.
//
// Group passages come last in a note rather than at the point they were
// spoken. A teacher's "everyone did well" is about the hour, not about the
// sentence before it.
func assemblePassages(passages []ExtractedPassage) ([]assembledNote, []JobPassage) {
	notes := []assembledNote{}
	at := map[string]int{}
	var group []string
	out := []JobPassage{}

	for _, p := range passages {
		if p.Kind == PassageNone {
			continue
		}
		// A straight conversion: the two types are the same fields in two
		// spellings, one the model's contract and one this API's. If they ever
		// stop being the same, this stops compiling, which is the point.
		out = append(out, JobPassage(p))
		switch {
		case p.Kind == PassageGroup:
			group = append(group, p.Summary)
		case p.Kind == PassageChild && p.Student != "":
			i, seen := at[p.Student]
			if !seen {
				at[p.Student] = len(notes)
				notes = append(notes, assembledNote{Name: p.Student, Summary: p.Summary, Passages: 1})
				continue
			}
			notes[i].Summary += "\n\n" + p.Summary
			notes[i].Passages++
		}
	}

	for _, g := range group {
		for i := range notes {
			notes[i].Summary += "\n\n" + g
			notes[i].Passages++
		}
	}
	return notes, out
}

// countKinds counts the passages of each kind, for the completion record.
func countKinds(passages []ExtractedPassage) map[PassageKind]int {
	counts := map[PassageKind]int{}
	for _, p := range passages {
		counts[p.Kind]++
	}
	return counts
}
