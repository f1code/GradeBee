// match.go resolves a spoken label ("Remy", "Joakim", "Max a Million") to a
// student in one class.
//
// The pipeline does not use it. Today's extractor is shown the roster and
// returns roster names itself (extract.go), so the only resolution left there
// is an id lookup. This is the second pass: when a recording reached no child
// and the teacher picks a class on the done card, handleAssembleNotes re-runs
// every spoken label against that class's roster — the labels are canonical
// against the roster the model was shown, which on that recording was the
// wrong one. It is pure, so match_test.go pins it against the eval fixtures'
// own rosters with no model run, and #125's two-pass contract resolves
// against it directly.
//
// Resolution order, measured in research rounds 5 and 9:
//
//  1. Exact match on a folded name or alias wins outright: no threshold, no
//     margin. An alias is the teacher's override and must never lose to a
//     heuristic. Two students sharing the string is a real tie → nobody.
//  2. Otherwise exact match on a whitespace-separated part of a name
//     (#111): a teacher says `Emma`, and against `Emma Torres` whole that
//     scores 0.40, so the child got no note at all. Parts come after the
//     typed strings so an alias never ties with a first name derived from
//     another child's name; two children sharing a first name is still a
//     tie. Aliases are not split — the teacher typed them as spoken. The
//     stop-list applies from here on: the teacher typed rule 1's strings,
//     the matcher derived these, and `He` is a part of `Wei He`.
//  3. Otherwise normalised Levenshtein over every name, part and alias, a
//     student's score being the best of their strings. Three gates, all
//     required: the label is not a pronoun; score >= 0.50; margin >= 0.15
//     over the best score of a *different* student. Parts under three
//     runes — `de`, an initial — sit this round out: any three-letter label
//     one edit away scores 0.67 against them.
//
// A number gates the fuzzy score (#111). Voxtral writes a spoken number as
// a digit, so `Arthur 1` on the roster is exact as spoken. A child whose
// name carries a number scores 0 against a label carrying a different
// one: with `Arthur 1` and `Arthur 2` in one class, `Artur 2` scores 0.86
// against both stems and would tie on the margin; gated, it meets
// `Arthur 2` alone. A bare `Arthur` carries no number, meets both, and
// ties. A child whose name carries no number is never gated.
//
// Breaking a tie by roster order would reproduce the root cause inside Go.
package handler

import (
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// matchThreshold admits Jill → Joël (0.50) and rejects Tilly →
	// Domitille (0.44), Val → Perceval (0.38).
	matchThreshold = 0.50
	// matchMargin rejects Ida (ties Ada / Ina at 0.67) and Sorine
	// (Simone 0.50 over Madeline 0.38).
	matchMargin = 0.15
)

// labelStopList holds folded labels that are never a name: the pronouns the
// teacher falls back on when a child is not named, plus the collective
// referents a label about a whole class might carry. Checked before any fuzzy
// score — `They` scores 0.75 against `Théo` with a 0.75 margin. `and` is
// here too: a label naming nobody but the conjunction (`Joakim and
// Adele` with a name dropped) would otherwise reach `Ana`, `Andy`, `Andre`
// and the rest of that name family at 0.67-0.86 (#112).
var labelStopList = map[string]bool{
	"he": true, "him": true, "his": true,
	"she": true, "her": true, "hers": true,
	"they": true, "them": true, "their": true, "theirs": true,
	"it": true, "its": true,
	"i": true, "me": true, "my": true, "mine": true,
	"we": true, "us": true, "our": true, "ours": true,
	"you": true, "your": true, "yours": true,
	"this": true, "that": true, "these": true, "those": true,
	"who": true, "someone": true, "somebody": true, "anyone": true, "anybody": true,
	"everyone": true, "everybody": true, "nobody": true, "noone": true,
	"all": true, "both": true, "one": true, "and": true,
}

// MatchStudent resolves label against the students of one class and returns
// the canonical roster name, or "" and false when nobody resolves. The
// caller must scope students to the pinned class: against the whole roster
// a level name like `Linda` outscores every true positive.
func MatchStudent(label string, students []ClassStudent) (string, bool) {
	key := FoldName(label)
	if key == "" {
		return "", false
	}

	keys := make([]studentKeys, len(students))
	for i, s := range students {
		keys[i] = keysOf(s)
	}

	// Rule 1: the strings the teacher typed, stop-list and all.
	if name, hit := exactOn(students, key, func(i int) []string { return keys[i].typed }); hit {
		return name, name != ""
	}

	// The matcher derived the parts, so a pronoun must not reach one:
	// `He` is a part of `Wei He`.
	if labelStopList[key] {
		return "", false
	}

	// Rule 2: parts of a name.
	if name, hit := exactOn(students, key, func(i int) []string { return keys[i].parts }); hit {
		return name, name != ""
	}

	labelNum := digitsOf(key)
	best, runnerUp, bestName := 0.0, 0.0, ""
	for i, s := range students {
		score := keys[i].bestScore(key, labelNum)
		switch {
		case score > best:
			runnerUp, best, bestName = best, score, s.Name
		case score > runnerUp:
			runnerUp = score
		}
	}
	if best < matchThreshold || best-runnerUp < matchMargin {
		return "", false
	}
	return bestName, true
}

// minFuzzyPartLen keeps particles and initials out of the fuzzy round.
const minFuzzyPartLen = 3

// studentKeys is every folded string a student answers to: the whole name
// and each alias as typed, and each whitespace-separated part of the name
// when there are several. Only the name is split — `Jean-Luc` stays one
// part, so `Jean` does not reach him. A part that is all digits is not a
// name: the `2` of `Arthur 2` is the number bestScore gates on.
type studentKeys struct {
	typed []string
	parts []string
	num   string // digits in the folded name, "" for most children
}

func keysOf(s ClassStudent) studentKeys {
	name := FoldName(s.Name)
	k := studentKeys{typed: []string{name}, num: digitsOf(name)}
	for _, a := range s.Aliases {
		k.typed = append(k.typed, FoldName(a))
	}
	if parts := strings.Fields(s.Name); len(parts) > 1 {
		for _, p := range parts {
			if f := FoldName(p); f != "" && digitsOf(f) != f {
				k.parts = append(k.parts, f)
			}
		}
	}
	return k
}

// exactOn reports whether any student's strings, as returned by keysAt for
// their index, equal key. One hit resolves to that student; two is a tie,
// reported as a hit with no name so the next tier is not consulted.
func exactOn(students []ClassStudent, key string, keysAt func(int) []string) (name string, hit bool) {
	for i, s := range students {
		if slices.Contains(keysAt(i), key) {
			if hit {
				return "", true // two students share the string
			}
			name, hit = s.Name, true
		}
	}
	return name, hit
}

// bestScore is the student's best similarity over their strings, or 0
// when the child and the label carry different numbers. A student's own
// second string never counts against them: the margin is taken between
// students, not between strings.
func (k studentKeys) bestScore(key, labelNum string) float64 {
	if k.num != "" && labelNum != "" && k.num != labelNum {
		return 0
	}
	best := 0.0
	for _, s := range k.typed {
		best = max(best, similarity(key, s))
	}
	for _, p := range k.parts {
		if len([]rune(p)) >= minFuzzyPartLen {
			best = max(best, similarity(key, p))
		}
	}
	return best
}

// FoldName lowercases, strips accents and drops everything that is not a
// letter or digit, so `Rémi` → `remi` and `Max a Million` → `maxamillion`.
func FoldName(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// digitsOf is the digits of a folded string in order, "" when none.
func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// similarity is normalised Levenshtein: 1 − distance / longer length, so
// identical strings score 1 and strings sharing nothing score 0.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	longest := max(len(ra), len(rb))
	if longest == 0 {
		return 0
	}
	return 1 - float64(Levenshtein(ra, rb))/float64(longest)
}

func Levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
