// voice_note_passages.go folds the passages extraction returned into the notes
// a recording produces and the passage list its done card carries.
//
// Pure: no repo, no model, no clock. The pipeline (voice_note_process.go) does
// the I/O around it, and the rules live here because they are the whole of
// what a recording means — which stretch of speech reaches which child, and
// which reaches nobody.
package handler

import "strings"

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
//   - child or absent with a roster student → that child's note. Several
//     passages about one child join in order; the model returns a shared
//     observation once per child, so a pair reaches both of them. An absent
//     child is a child the teacher spoke about, so "Théo was absent today" is
//     his note.
//   - child or absent with no student → nobody. Its labels stay on the
//     passage, which is what the class picker re-resolves when the recording
//     was read against the wrong roster.
//   - unknown → nobody, and no labels to re-resolve: the recording never said
//     who this was.
//   - group → every child on roster, the pinned class's children, except a
//     child with an absent passage: silence is presence, so a child the
//     teacher never mentioned was there for "everyone did well". Suppressed
//     entirely when the recording spoke a name and none resolved — it was read
//     against the wrong class, and fanning out would write notes for that
//     whole roster and close the class picker, which the card gates on the
//     note count. An absent passage counts as resolving. hasSpokenName decides
//     "spoke a name", the rule noNotesReason and the pronoun guard use.
//   - none → dropped, and not on the card. A recording holding nothing but a
//     spoken header yields no passages, so it reads as nobody named instead of
//     offering the class picker over a passage there is nothing to pick for.
//
// Notes come in the order the teacher first spoke each child, then children
// reached only by group passages in roster order. A child with their own
// passages gets the group text whatever roster holds, so a nil roster — a
// failed roster read — degrades to reaching only the children named.
//
// Group passages come last in a note rather than at the point they were
// spoken. A teacher's "everyone did well" is about the hour, not about the
// sentence before it.
func assemblePassages(passages []ExtractedPassage, roster []ClassStudent) ([]assembledNote, []JobPassage) {
	var names []string
	own := map[string][]string{}
	absent := map[string]bool{}
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
		case (p.Kind == PassageChild || p.Kind == PassageAbsent) && p.Student != "":
			if _, seen := own[p.Student]; !seen {
				names = append(names, p.Student)
			}
			own[p.Student] = append(own[p.Student], p.Summary)
			if p.Kind == PassageAbsent {
				absent[foldName(p.Student)] = true
			}
		}
	}

	if len(names) == 0 && anySpokenLabel(out) {
		group = nil
	}

	notes := make([]assembledNote, 0, len(names))
	reached := map[string]bool{}
	for _, name := range names {
		reached[foldName(name)] = true
		g := group
		if absent[foldName(name)] {
			g = nil
		}
		notes = append(notes, assembledNote{
			Name:     name,
			Summary:  joinPassageText(own[name], g),
			Passages: len(own[name]) + len(g),
		})
	}
	if len(group) == 0 {
		return notes, out
	}
	// An absent child always has an own passage, so reached covers them.
	for _, s := range roster {
		if reached[foldName(s.Name)] {
			continue
		}
		reached[foldName(s.Name)] = true
		notes = append(notes, assembledNote{
			Name:     s.Name,
			Summary:  joinPassageText(nil, group),
			Passages: len(group),
		})
	}
	return notes, out
}

// joinPassageText is the text of a note built from passages: the child's own
// summaries in the order given, then the recording's class-wide ones. Blank
// line between them: they are separate stretches of speech, and running them
// together invents a sentence the teacher never said.
//
// One helper for both writers — the pipeline's fold above and the assign
// endpoint, where the teacher picks the child — so a note reads the same
// whoever named the child.
func joinPassageText(own, group []string) string {
	parts := make([]string, 0, len(own)+len(group))
	parts = append(parts, own...)
	parts = append(parts, group...)
	return strings.Join(parts, "\n\n")
}

// countKinds counts the passages of each kind, for the completion record.
func countKinds(passages []ExtractedPassage) map[PassageKind]int {
	counts := map[PassageKind]int{}
	for _, p := range passages {
		counts[p.Kind]++
	}
	return counts
}
