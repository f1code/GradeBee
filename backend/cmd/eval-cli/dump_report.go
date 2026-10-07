package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	handler "github.com/nicogaller/gradebee/backend"
)

// runDumpReport prints one report's notes and reference HTML with the class
// roster's names redacted, misspellings included, so an agent can read it to pick and describe cases.
// Terminal only: the text still carries sensitive non-name content.
func runDumpReport(args []string) error {
	fls := flag.NewFlagSet("dump-report", flag.ContinueOnError)
	dbPath := fls.String("db", "", "SQLite DB to read the report from")
	studentID := fls.Int64("student", 0, "student id")
	reportID := fls.Int64("report", 0, "report id")
	replPath := fls.String("replacements", "", "append each fuzzy replacement (word, token) to this file; it holds names")
	if err := fls.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *studentID <= 0 || *reportID <= 0 {
		return fmt.Errorf("usage: eval-cli dump-report -db PATH -student N -report M [-replacements FILE]")
	}
	if _, err := os.Stat(*dbPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	db, err := handler.OpenDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	var repl io.Writer
	if *replPath != "" {
		f, err := os.OpenFile(*replPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		repl = f
	}
	return dumpReport(context.Background(), db, os.Stdout, repl, *studentID, *reportID)
}

// dumpReport writes the redacted report to w and, when replacements is not
// nil, each fuzzy-matched word with its token, for a human to check.
func dumpReport(ctx context.Context, db *sql.DB, w, replacements io.Writer, studentID, reportID int64) error {
	resolver := handler.NewReportInputResolver(db)
	in, rpt, err := resolver.ForReport(ctx, "", studentID, reportID)
	if err != nil {
		return err
	}
	roster, err := resolver.ClassRoster(ctx, studentID)
	if err != nil {
		return err
	}
	r := newRedactor(roster, studentID)

	texts := []string{in.Instructions, rpt.HTML}
	for _, n := range in.Notes {
		texts = append(texts, n.Summary)
	}
	for i, t := range texts {
		texts[i] = r.redact(t)
	}
	openers := r.noteOpeners(texts[2:])
	for i := 2; i < len(texts); i++ {
		texts[i] = replaceWords(texts[i], openers)
	}
	texts, fuzzed := r.fuzzyRedact(texts)
	maps.Copy(fuzzed, openers)
	if replacements != nil {
		words := slices.Sorted(maps.Keys(fuzzed))
		fmt.Fprintf(replacements, "# report %d, student %d\n", reportID, studentID)
		for _, word := range words {
			fmt.Fprintf(replacements, "%s\t%s\n", word, fuzzed[word])
		}
		fmt.Fprintf(os.Stderr, "report %d: %d fuzzy replacements\n", reportID, len(words))
	}

	fmt.Fprintf(w, "Report %d, student %d, %s..%s, %d notes\n", reportID, studentID, in.StartDate, in.EndDate, len(in.Notes))
	if strings.TrimSpace(texts[0]) != "" {
		fmt.Fprintf(w, "\n## Ad-hoc instructions\n\n%s\n", texts[0])
	}
	fmt.Fprintln(w, "\n## Notes")
	for i, n := range in.Notes {
		fmt.Fprintf(w, "\n%s: %s\n", n.Date, texts[i+2])
	}
	fmt.Fprintf(w, "\n## Reference report\n\n%s\n", texts[1])
	return nil
}

// wordRe matches letter/digit runs; combining marks stay inside a word.
var wordRe = regexp.MustCompile(`[\pL\p{Nd}][\pL\p{Nd}\p{Mn}]*`)

type redactTerm struct {
	folded []rune
	token  string
}

// redactor replaces roster names and aliases, longest first, so a full name
// wins over an alias that prefixes it. fuzzy holds what fuzzyRedact compares
// against, folded as match.go folds: each part of a name, each alias whole.
type redactor struct{ terms, fuzzy []redactTerm }

// newRedactor maps the student's name and aliases to STUDENT and each
// classmate's to CLASSMATE_n, numbered in roster order.
func newRedactor(roster []handler.Student, studentID int64) *redactor {
	var terms, fuzzy []redactTerm
	seen := map[string]bool{}
	add := func(s handler.Student, token string) {
		for _, t := range append(strings.Fields(s.Name), s.Aliases...) {
			if f := handler.FoldName(t); f != "" {
				fuzzy = append(fuzzy, redactTerm{[]rune(f), token})
			}
		}
		for _, t := range append([]string{s.Name}, s.Aliases...) {
			f, _, _ := foldText(strings.TrimSpace(t))
			if len(f) == 0 || seen[string(f)] {
				continue
			}
			seen[string(f)] = true
			terms = append(terms, redactTerm{f, token})
		}
	}
	for _, s := range roster {
		if s.ID == studentID {
			add(s, "STUDENT")
		}
	}
	n := 0
	for _, s := range roster {
		if s.ID != studentID {
			n++
			add(s, fmt.Sprintf("CLASSMATE_%d", n))
		}
	}
	sort.SliceStable(terms, func(i, j int) bool { return len(terms[i].folded) > len(terms[j].folded) })
	return &redactor{terms: terms, fuzzy: fuzzy}
}

// foldText lowercases s, strips accents (composed, combining or stroke), spells out ligatures and
// compatibility forms, and collapses runs of spaces and dashes to one space,
// so "Élodie", "ELODIE" and "Elodie" compare equal, as do "Jean-Luc" and
// "Jean  Luc". For each folded rune it returns the byte range of the source
// runes, trailing combining marks included, so a match maps back onto s.
func foldText(s string) (folded []rune, starts, ends []int) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case unicode.Is(unicode.Mn, r):
			if len(ends) > 0 {
				ends[len(ends)-1] = i
			}
			continue
		case unicode.IsSpace(r) || unicode.Is(unicode.Pd, r):
			if n := len(folded); n > 0 && folded[n-1] == ' ' {
				ends[n-1] = i
				continue
			}
			folded = append(folded, ' ')
			starts = append(starts, i-size)
			ends = append(ends, i)
			continue
		}
		for _, d := range norm.NFKD.String(string(unicode.ToLower(r))) {
			if unicode.Is(unicode.Mn, d) {
				continue
			}
			f, ok := letterFold[d]
			if !ok {
				f = string(d)
			}
			for _, e := range f {
				folded = append(folded, e)
				starts = append(starts, i-size)
				ends = append(ends, i)
			}
		}
	}
	return folded, starts, ends
}

// letterFold maps lowercase letters NFKD leaves whole to their base spelling.
var letterFold = map[rune]string{
	'ø': "o", 'ł': "l", 'đ': "d", 'ħ': "h", 'ŧ': "t", 'ı': "i",
	'ß': "ss", 'œ': "oe", 'æ': "ae",
}

// redact matches whole words on folded text. Go's \b is ASCII-only and would
// miss names starting or ending with an accented letter.
func (r *redactor) redact(s string) string {
	f, starts, ends := foldText(s)
	var b strings.Builder
	copied := 0
	for i := 0; i < len(f); i++ {
		if i > 0 && (isWordRune(f[i-1]) || starts[i] == starts[i-1]) {
			continue
		}
		t, ok := r.matchAt(f, starts, i)
		if !ok {
			continue
		}
		end := i + len(t.folded)
		b.WriteString(s[copied:starts[i]])
		b.WriteString(t.token)
		copied = ends[end-1]
		i = end - 1
	}
	b.WriteString(s[copied:])
	return b.String()
}

func (r *redactor) matchAt(f []rune, starts []int, i int) (redactTerm, bool) {
	for _, t := range r.terms {
		end := i + len(t.folded)
		if end > len(f) || string(f[i:end]) != string(t.folded) {
			continue
		}
		if end == len(f) || (!isWordRune(f[end]) && starts[end] != starts[end-1]) {
			return t, true
		}
	}
	return redactTerm{}, false
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// noteOpeners maps each note's first word to STUDENT: a filed note opens with
// this student's name, however misheard. One closer to a classmate is left to
// fuzzyRedact. Notes only; reports spell the roster name.
func (r *redactor) noteOpeners(notes []string) map[string]string {
	seed := map[string]string{}
	for _, n := range notes {
		w := wordRe.FindString(n)
		first, _ := utf8.DecodeRuneInString(w)
		if !unicode.IsUpper(first) || ownTokens[w] || openerStopWords[strings.ToLower(w)] {
			continue
		}
		if tok := r.closest(w); tok == "" || tok == "STUDENT" {
			seed[w] = "STUDENT"
		}
	}
	return seed
}

func replaceWords(s string, tokens map[string]string) string {
	return wordRe.ReplaceAllStringFunc(s, func(w string) string {
		if tok := tokens[w]; tok != "" {
			return tok
		}
		return w
	})
}

// openerStopWords open a note without naming anyone. A fixed list, not
// fuzzyRedact's lowercase check, which leaks names that are also words.
var openerStopWords = map[string]bool{
	"she": true, "he": true, "they": true, "the": true, "today": true, "this": true,
	"it": true, "i": true, "we": true, "a": true, "an": true, "recently": true, "then": true,
	"her": true, "his": true, "in": true, "during": true, "when": true, "very": true,
	"great": true, "after": true, "also": true,
}

// fuzzyRedact catches roster names the exact pass misses: misspellings and
// mistranscriptions. A capitalized word within editLimit of a roster term
// takes that person's token at every occurrence, or CLASSMATE_? when two
// people are equally close, so a reviewer never reads a wrong attribution.
// A word whose lowercase form occurs in the dump is a common word: "Then"
// beside roster Theo. Returns the texts and each replaced word's token.
func (r *redactor) fuzzyRedact(texts []string) (out []string, tokens map[string]string) {
	lower := map[string]bool{}
	for _, t := range texts {
		for _, w := range wordRe.FindAllString(t, -1) {
			if strings.ToLower(w) == w {
				lower[w] = true
			}
		}
	}
	tokens = map[string]string{}
	for _, t := range texts {
		for _, w := range wordRe.FindAllString(t, -1) {
			first, _ := utf8.DecodeRuneInString(w)
			if _, done := tokens[w]; done || !unicode.IsUpper(first) || ownTokens[w] || lower[strings.ToLower(w)] {
				continue
			}
			if tok := r.closest(w); tok != "" {
				tokens[w] = tok
			}
		}
	}
	out = make([]string, len(texts))
	for i, t := range texts {
		out[i] = replaceWords(t, tokens)
	}
	return out, tokens
}

// ownTokens are the words of the redactor's tokens: a roster name near
// STUDENT or CLASSMATE must not rewrite a token.
var ownTokens = map[string]bool{"STUDENT": true, "CLASSMATE": true}

// closest returns the token of the one person whose term lies within
// editLimit of word, the nearest if several, "CLASSMATE_?" on a tie and ""
// when none does.
func (r *redactor) closest(word string) string {
	w := []rune(handler.FoldName(word))
	best := map[string]int{}
	for _, t := range r.fuzzy {
		d := handler.Levenshtein(w, t.folded)
		if d > editLimit(len(t.folded)) {
			continue
		}
		if prev, ok := best[t.token]; !ok || d < prev {
			best[t.token] = d
		}
	}
	tok, nearest := "", -1
	for t, d := range best {
		switch {
		case nearest < 0 || d < nearest:
			tok, nearest = t, d
		case d == nearest:
			tok = "CLASSMATE_?"
		}
	}
	return tok
}

// editLimit is the edit distance allowed against a roster term of n runes:
// any edit to a short name lands on common words ("And" vs Ann).
func editLimit(n int) int {
	switch {
	case n <= 3:
		return 0
	case n <= 6:
		return 1
	}
	return 2
}
