package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	handler "github.com/nicogaller/gradebee/backend"
)

// runDumpReport prints one report's notes and reference HTML with the class
// roster's names redacted, so an agent can read it to pick and describe cases.
// Terminal only: the text still carries sensitive non-name content.
func runDumpReport(args []string) error {
	fls := flag.NewFlagSet("dump-report", flag.ContinueOnError)
	dbPath := fls.String("db", "", "SQLite DB to read the report from")
	studentID := fls.Int64("student", 0, "student id")
	reportID := fls.Int64("report", 0, "report id")
	if err := fls.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *studentID <= 0 || *reportID <= 0 {
		return fmt.Errorf("usage: eval-cli dump-report -db PATH -student N -report M")
	}
	if _, err := os.Stat(*dbPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	db, err := handler.OpenDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return dumpReport(context.Background(), db, os.Stdout, *studentID, *reportID)
}

func dumpReport(ctx context.Context, db *sql.DB, w io.Writer, studentID, reportID int64) error {
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

	fmt.Fprintf(w, "Report %d, student %d, %s..%s, %d notes\n", reportID, studentID, in.StartDate, in.EndDate, len(in.Notes))
	if strings.TrimSpace(in.Instructions) != "" {
		fmt.Fprintf(w, "\n## Ad-hoc instructions\n\n%s\n", r.redact(in.Instructions))
	}
	fmt.Fprintln(w, "\n## Notes")
	for _, n := range in.Notes {
		fmt.Fprintf(w, "\n%s: %s\n", n.Date, r.redact(n.Summary))
	}
	fmt.Fprintf(w, "\n## Reference report\n\n%s\n", r.redact(rpt.HTML))
	return nil
}

type redactTerm struct {
	folded []rune
	token  string
}

// redactor replaces roster names and aliases, longest first, so a full name
// wins over an alias that prefixes it.
type redactor struct{ terms []redactTerm }

// newRedactor maps the student's name and aliases to STUDENT and each
// classmate's to CLASSMATE_n, numbered in roster order.
func newRedactor(roster []handler.Student, studentID int64) *redactor {
	var terms []redactTerm
	seen := map[string]bool{}
	add := func(s handler.Student, token string) {
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
	return &redactor{terms: terms}
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
