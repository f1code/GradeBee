package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func students(names ...string) []ClassStudent {
	out := make([]ClassStudent, len(names))
	for i, n := range names {
		out[i] = ClassStudent{Name: n}
	}
	return out
}

func aliased(name string, aliases ...string) ClassStudent {
	return ClassStudent{Name: name, Aliases: aliases}
}

func TestFoldName(t *testing.T) {
	cases := map[string]string{
		"Rémi":                 "remi",
		"Héloïse":              "heloise",
		"Thaïs":                "thais",
		"Max a Million":        "maxamillion",
		"O'Brien":              "obrien",
		"  Loïc  ":             "loic",
		"Jean-Luc":             "jeanluc",
		"CÉLESTINE":            "celestine",
		"1745":                 "1745",
		"":                     "",
		" - ":                  "",
		"Ben & Brenda · Larry": "benbrendalarry",
		"Arthur 1":             "arthur1",
		"Arthur 1.":            "arthur1",
		"Arthur one":           "arthurone", // not folded: Voxtral writes digits
	}
	for in, want := range cases {
		assert.Equal(t, want, FoldName(in), "%q", in)
	}
}

func TestSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"remi", "remi", 1.0},
		{"remy", "remi", 0.75},
		{"joakim", "joachim", 1 - 2.0/7},
		{"jill", "joel", 0.5},
		{"", "joel", 0.0},
		{"", "", 0.0},
		{"abc", "xyz", 0.0},
	}
	for _, tc := range cases {
		assert.InDelta(t, tc.want, similarity(tc.a, tc.b), 1e-9, "%q vs %q", tc.a, tc.b)
	}
}

// Rosters shaped like the research rounds' classes, first names only (names
// are made up). Two classes share a level name and a weekday, which is why
// the matcher takes one class's students rather than the roster. "Oliver ·
// Thu" is reconstructed from the research scores (Raoul → Raul 0.80, Erwin →
// Erwan 0.80).
var (
	rosterLindaSat0855 = students("Katharine", "Rosalind", "Tobias")
	rosterLindaSat1005 = students("Gaston", "Joël", "Ondine", "Tristan")
	rosterLindaWed1315 = students("Bruno", "Theodore")
	rosterLindaWed1745 = students("Domitille", "Héloïse", "Rémi")
	rosterMousySat1115 = students("Ghislaine")
	rosterMousySat1225 = students("Ivy")
	rosterMousyThu1715 = students("Seraphina")
	rosterOliverThu    = students("Anouk", "Edgar", "Erwan", "Lucien", "Raul")
	rosterPPFri1740    = students("Elka", "Elsa", "Judith", "Madeline", "Marthe", "Nora", "Simone", "Thaïs")
	rosterPPWed1410    = students("Ada", "Adela", "Bettina", "Héloïse", "Elvire", "Joachim", "Lise", "Tristan")
	rosterPPWed1520    = students("Aurélie", "Edmée", "Elio", "Ewen", "Gauvain", "Oona", "Silas", "Tristan")
	rosterPPWed1630    = []ClassStudent{
		{Name: "Maximilien"}, aliased("Ina", "Ida"), {Name: "Ada"},
		{Name: "Célestine"}, {Name: "Malo"}, {Name: "Raoul"}, {Name: "Loïc"},
	}
	rosterSamFri1630 = []ClassStudent{
		aliased("Perceval", "Val"), {Name: "Célestine"}, {Name: "Gisèle"}, {Name: "Hermine"}, {Name: "Ninon"},
	}
)

type matchCase struct {
	label string
	class []ClassStudent
	want  string // "" = nobody
}

// TestMatchStudent_Corpus pins every (label, class) pair the segmentation
// model returned over the 22-transcript corpus (research round 9), matched
// within the class the model pinned. `Max` is tested on its own below.
func TestMatchStudent_Corpus(t *testing.T) {
	cases := []matchCase{
		{"Rosalind", rosterLindaSat0855, "Rosalind"},
		{"Katherine", rosterLindaSat0855, "Katharine"}, // 0.89
		{"Tobias", rosterLindaSat0855, "Tobias"},

		{"Ondine", rosterLindaSat1005, "Ondine"},
		{"Jill", rosterLindaSat1005, "Joël"}, // 0.50, exactly at threshold; runner-up 0.17
		{"Gaston", rosterLindaSat1005, "Gaston"},
		{"Tristan", rosterLindaSat1005, "Tristan"},

		{"Theodore", rosterLindaWed1315, "Theodore"},
		{"Bruno", rosterLindaWed1315, "Bruno"},

		{"Remy", rosterLindaWed1745, "Rémi"}, // 0.75
		{"Tilly", rosterLindaWed1745, ""},    // Domitille 0.44, below threshold
		{"She", rosterLindaWed1745, ""},      // stop-list

		{"Ghislaine", rosterMousySat1115, "Ghislaine"},
		{"Ivy", rosterMousySat1225, "Ivy"},
		{"She", rosterMousySat1225, ""},
		{"Serafina", rosterMousyThu1715, "Seraphina"}, // 0.78, sole student

		{"Anouk", rosterOliverThu, "Anouk"},
		{"Lucien", rosterOliverThu, "Lucien"},
		{"Edgar", rosterOliverThu, "Edgar"},
		{"Raoul", rosterOliverThu, "Raul"},  // 0.80
		{"Erwin", rosterOliverThu, "Erwan"}, // 0.80

		{"Ella", rosterPPFri1740, ""}, // Elka 0.75 ties Elsa 0.75: margin 0
		{"Elka", rosterPPFri1740, "Elka"},
		{"Thais", rosterPPFri1740, "Thaïs"}, // exact after fold
		{"Madeleine", rosterPPFri1740, "Madeline"},
		{"Marthe", rosterPPFri1740, "Marthe"},
		{"Nora", rosterPPFri1740, "Nora"},
		{"Judy", rosterPPFri1740, "Judith"},
		{"Sorine", rosterPPFri1740, ""}, // Simone 0.50 over Madeline 0.38: margin 0.12

		{"Adah", rosterPPWed1410, "Ada"},
		{"Adele", rosterPPWed1410, "Adela"},    // 0.80, Ada 0.40
		{"Eloise", rosterPPWed1410, "Héloïse"}, // 0.86
		{"Lise", rosterPPWed1410, "Lise"},
		{"Tristan", rosterPPWed1410, "Tristan"},
		{"Elvire", rosterPPWed1410, "Elvire"},
		{"Bettina", rosterPPWed1410, "Bettina"},
		{"Joakim", rosterPPWed1410, "Joachim"}, // 0.71, Ada 0.17

		{"Aurelia", rosterPPWed1520, "Aurélie"},
		{"Aurélie", rosterPPWed1520, "Aurélie"},
		{"Edmee", rosterPPWed1520, "Edmée"},
		{"Gauvin", rosterPPWed1520, "Gauvain"},
		{"Silas", rosterPPWed1520, "Silas"},
		{"Tristan", rosterPPWed1520, "Tristan"},
		{"Owen", rosterPPWed1520, "Ewen"}, // 0.75, Oona 0.25
		{"Elio", rosterPPWed1520, "Elio"},
		{"Ona", rosterPPWed1520, "Oona"},

		{"Maximilien", rosterPPWed1630, "Maximilien"},
		{"Ida", rosterPPWed1630, "Ina"}, // alias, exact
		{"Celestine", rosterPPWed1630, "Célestine"},
		{"Celeste", rosterPPWed1630, "Célestine"}, // 0.78
		{"Malo", rosterPPWed1630, "Malo"},
		{"Raoul", rosterPPWed1630, "Raoul"},
		{"Adah", rosterPPWed1630, "Ada"},  // 0.75, Ina 0.50
		{"Lois", rosterPPWed1630, "Loïc"}, // 0.75

		{"Celestine", rosterSamFri1630, "Célestine"},
		{"Her", rosterSamFri1630, ""},           // stop-list; Hermine 0.43 anyway
		{"Giselle", rosterSamFri1630, "Gisèle"}, // 0.86
		{"Nino", rosterSamFri1630, "Ninon"},     // 0.80, Hermine 0.29
		{"Val", rosterSamFri1630, "Perceval"},   // alias, exact; fuzzy would be 0.38
	}
	assert.Len(t, cases, 59)
	runMatchCases(t, cases)
}

func runMatchCases(t *testing.T, cases []matchCase) {
	t.Helper()
	for _, tc := range cases {
		got, ok := MatchStudent(tc.label, tc.class)
		assert.Equal(t, tc.want, got, "label %q", tc.label)
		assert.Equal(t, tc.want != "", ok, "label %q ok", tc.label)
	}
}

// TestMatchStudent_VerbatimLabelBeatsTidiedLabel: the model once returned
// `Max` for the spoken "Max a Million". Verbatim, it reaches Maximilien;
// tidied, it lands on Malo at 0.50 with a 0.20 margin — the accepted false
// positive of threshold 0.50 (plan decision 1), guarded by the prompt's
// verbatim-label rule rather than by the matcher. Pinned so a threshold
// change surfaces it deliberately.
func TestMatchStudent_VerbatimLabelBeatsTidiedLabel(t *testing.T) {
	runMatchCases(t, []matchCase{
		{"Max a Million", rosterPPWed1630, "Maximilien"},
		{"Max", rosterPPWed1630, "Malo"},
	})
}

// TestMatchStudent_Gates exercises each gate on its own: threshold, margin,
// stop-list, and the exact short-circuit that bypasses all three.
func TestMatchStudent_Gates(t *testing.T) {
	runMatchCases(t, []matchCase{
		// Ida ties Ada and Ina at 0.67 with no alias to break it.
		{"Ida", students("Maximilien", "Ina", "Ada"), ""},
		// The same label resolves outright once the teacher adds the alias,
		// even though Ada at 0.67 would fail the margin on the fuzzy path.
		{"Ida", []ClassStudent{{Name: "Maximilien"}, aliased("Ina", "Ida"), {Name: "Ada"}}, "Ina"},
		// Alias, exact: fuzzy alone rejects Val → Perceval at 0.38.
		{"Val", students("Perceval", "Célestine"), ""},
		{"Val", []ClassStudent{aliased("Perceval", "Val"), {Name: "Célestine"}}, "Perceval"},
		// Near-misses of an alias go through the fuzzy path over the alias.
		{"Vahl", []ClassStudent{aliased("Perceval", "Val"), {Name: "Célestine"}, {Name: "Gisèle"}}, "Perceval"},
		{"Idda", []ClassStudent{{Name: "Maximilien"}, aliased("Ina", "Ida"), {Name: "Ada"}}, "Ina"},
		// A student's own second string never counts as the runner-up.
		{"Idda", []ClassStudent{aliased("Ina", "Ida", "Idha")}, "Ina"},
		// Stop-list beats a score that passes both other gates: They → Théo
		// is 0.75 with a 0.75 margin.
		{"They", students("Théo"), ""},
		{"they", students("Théo"), ""},
		{"THEY", students("Théo"), ""},
		{"He", students("Hector"), ""},
		{"everyone", students("Evelyn"), ""},
		{"one", students("Oney", "Arthur 1"), ""},
		// A stranded "and" (a fused label with a name dropped) would
		// otherwise reach Ana at 0.67 (#112).
		{"and", students("Ana", "Bram"), ""},
		// Case and accents never matter.
		{"remi", students("Rémi"), "Rémi"},
		{"RÉMI", students("Rémi"), "Rémi"},
		// Nothing to match.
		{"", students("Rémi"), ""},
		{" - ", students("Rémi"), ""},
		{"Rémi", nil, ""},
		{"Rémi", students(), ""},
	})
}

// TestMatchStudent_SharedStringIsATie: two students sharing a name or an
// alias in one class resolve to nobody, on the exact path and on the fuzzy
// path alike. Breaking the tie by roster order would be the bug (#99) again.
func TestMatchStudent_SharedStringIsATie(t *testing.T) {
	runMatchCases(t, []matchCase{
		{"Tristan", students("Tristan", "Tristan"), ""},
		{"Tristan", students("Tristan", "Silas"), "Tristan"}, // same label, no tie
		{"Ali", []ClassStudent{aliased("Ina", "Ali"), aliased("Ada", "Ali")}, ""},
		{"Ali", []ClassStudent{aliased("Ina", "Ali"), {Name: "Ada"}}, "Ina"},
		// One student's alias equals another student's name.
		{"Ada", []ClassStudent{aliased("Ina", "Ada"), {Name: "Ada"}}, ""},
		// Fuzzy path: the two exact twins score the same, so margin is 0.
		{"Tristen", students("Tristan", "Tristan"), ""},
		{"Tristen", students("Tristan", "Silas"), "Tristan"},
	})
}

// TestMatchStudent_FullNameRoster: a teacher says the first name; four
// production students and every full name added from now on are `First
// Last`. Whole-name scoring gave Emma → Emma Torres 0.40 and no note at all
// (#111), so each whitespace part of the roster name is a string of its own:
// exact after the typed strings, fuzzy alongside them. The tie rule is
// unchanged: two children sharing a first name in one class resolve to
// nobody.
func TestMatchStudent_FullNameRoster(t *testing.T) {
	classA := students("Emma Torres", "Ryan Mitchell", "Lila Patel")
	runMatchCases(t, []matchCase{
		{"Emma", classA, "Emma Torres"},
		{"Ryan", classA, "Ryan Mitchell"},
		{"Noah", students("Noah Jensen", "Mia Clark"), "Noah Jensen"},
		{"Olivia", students("Olivia Chen", "Marcus Davis", "Zoe Taylor"), "Olivia Chen"},
		// The whole name and a surname alone still reach the child.
		{"Emma Torres", classA, "Emma Torres"},
		{"Torres", classA, "Emma Torres"},
		// A middle part counts too.
		{"Rose", students("Anna Rose Lee", "Tom"), "Anna Rose Lee"},
		// Fuzzy path over a part: Emme → emma 0.75, Ryan Mitchell's best 0.17.
		{"Emme", classA, "Emma Torres"},
		// A part is exact, so it wins outright: fuzzy alone would give
		// Isabelle Brown 1.0 over Isabella 0.875, under the margin.
		{"Isabelle", students("Isabelle Brown", "Isabella"), "Isabelle Brown"},
		// Ties: two full names sharing a first name, on both paths.
		{"Emma", students("Emma Torres", "Emma Wilson"), ""},
		{"Emme", students("Emma Torres", "Emma Wilson"), ""},
		{"Emma", students("Emma Torres", "Emma Wilson", "Ryan Mitchell"), ""},
		// A shared surname is a tie the same way.
		{"Torres", students("Emma Torres", "Luis Torres"), ""},
		// Typed strings come first: a whole name or an alias equal to the
		// label beats a part derived from another child's name, so the
		// teacher can break a first-name tie with an alias.
		{"Emma", students("Emma", "Emma Torres"), "Emma"},
		{"Morgan", students("Jack Morgan", "Morgan"), "Morgan"},
		{"Emma", []ClassStudent{{Name: "Emma Torres"}, aliased("Bea", "Emma")}, "Bea"},
		{"Emma", []ClassStudent{{Name: "Emma Torres"}, aliased("Emma Wilson", "Emma")}, "Emma Wilson"},
		// Only the name is split: a hyphen is one part, so `Jean` does not
		// tie with Jean-Luc.
		{"Jean", students("Jean", "Jean-Luc"), "Jean"},
		{"Luc", students("Jean-Luc Picard"), ""}, // luc → jeanluc 0.43, picard 0.17
		// Parts under three runes are exact-only: `de` would score 0.67
		// against `Dee` on the fuzzy path, and `Li` still reaches Li Wei.
		{"Dee", students("Maria de la Cruz", "Sam"), ""},
		{"Li", students("Li Wei", "Sam"), "Li Wei"},
		// Stop-list gates both the exact and the fuzzy path over a part:
		// the matcher derived `he` from `Wei He`, nobody typed it.
		{"He", students("Wei He", "Sam"), ""},
		{"I", students("Anna I Smith"), ""},
		{"They", students("Théo Martin"), ""},
		// A typed string still bypasses it, as before.
		{"He", []ClassStudent{aliased("Hector", "He")}, "Hector"},
	})
}

// TestMatchStudent_NumberedNames: two children with the same first name are
// enrolled as `Arthur 1` and `Arthur 2`. Voxtral writes a spoken number as
// a digit, so the name is exact as spoken; a child carrying a number
// scores 0 against a label carrying a different one, so `Artur 2` meets
// `Arthur 2` alone where it scored 0.86 against both and tied. A bare
// `Arthur` still ties.
func TestMatchStudent_NumberedNames(t *testing.T) {
	twins := students("Arthur 1", "Arthur 2", "Lucie")
	runMatchCases(t, []matchCase{
		{"Arthur 1", twins, "Arthur 1"},
		{"Arthur 2", twins, "Arthur 2"},
		{"Arthur 2.", twins, "Arthur 2"},
		{"Artur 2", twins, "Arthur 2"}, // 0.86, Arthur 1 gated to 0
		{"Artur 1", twins, "Arthur 1"},
		// No number: both twins score 0.86 on the whole and 1.0 on the
		// part, tie.
		{"Arthur", twins, ""},
		{"Artur", twins, ""},
		// A number nobody carries gates both twins to 0.
		{"Arthur 3", twins, ""},
		// A child without a number is never gated: lucy2 → lucie 0.60.
		{"Lucy 2", twins, "Lucie"},
		{"Lucie", twins, "Lucie"},
		// A typed alias still wins outright, numbered or not, and even
		// when another child carries that number.
		{"Tutu", []ClassStudent{{Name: "Arthur 1"}, aliased("Arthur 2", "Tutu")}, "Arthur 2"},
		{"Arthur 2", []ClassStudent{aliased("Arthur Dupont", "Arthur 2"), {Name: "Bob 2"}, {Name: "Bob 1"}}, "Arthur Dupont"},
		// A near name elsewhere in the class is gated like anyone else.
		{"Artur 2", students("Arthur 1", "Arthur 2", "Arthus"), "Arthur 2"},
		{"Artus", students("Arthur 1", "Arthur 2", "Arthus"), "Arthus"},
		// A number alone is not a name: `2` is no part.
		{"2", twins, ""},
		{"1745", students("1745"), "1745"},
		// Number words are not folded: Voxtral writes digits (probe,
		// 2026-09-03). `arthurone` scores 0.67 against both twins, tie.
		{"Arthur one", twins, ""},
	})
}

// fusedJoiner matches the ways a teacher joins two names in one breath —
// the same shapes the removed splitLabel used to repair. Production code
// no longer cares; the live guard (TestExtractTwoChildrenOneObservationLabelsEach,
// llm_live_test.go) still needs to tell a fused label from a clean one to
// catch the model regressing (#112).
var fusedJoiner = regexp.MustCompile(`(?i)[,;&/]|\s+and\s+`)

// looksFused reports whether label sounds like it names more than one child.
func looksFused(label string) bool {
	return fusedJoiner.MatchString(label)
}

// TestLooksFused pins the guard itself: a typo in fusedJoiner would leave
// TestExtractTwoChildrenOneObservationLabelsEach silently green forever,
// since only a live model run exercises it.
func TestLooksFused(t *testing.T) {
	cases := map[string]bool{
		"Joakim and Adele":         true,
		"Tristan, Eloise and Lise": true,
		"Emma & Ryan":              true,
		"Emma/Ryan":                true,
		"Owen; Elio":               true,
		"Bruno AND Theodore":       true, // case-insensitive
		"Anne-and-Marie":           false,
		"Andrea":                   false,
		"Anna Rose Lee":            false,
		"Emma":                     false,
		"Andy and":                 false, // a stranded conjunction, not a fused label
	}
	for label, want := range cases {
		assert.Equal(t, want, looksFused(label), "%q", label)
	}
}

// TestMatchStudent_MiddleNamesAndInitials: a teacher tells two children
// with one first name apart by a middle name or an initial. Voxtral writes
// a spoken initial as the letter, with or without a period (probe,
// 2026-09-03), so the whole name is exact; a bare first name is a tie.
func TestMatchStudent_MiddleNamesAndInitials(t *testing.T) {
	initials := students("Emma T", "Emma R", "Lucie")
	middles := students("Emma Rose", "Emma Louise", "Lucie")
	runMatchCases(t, []matchCase{
		{"Emma T", initials, "Emma T"},
		{"Emma T.", initials, "Emma T"},
		{"Emma R", initials, "Emma R"},
		{"Emmy T", initials, "Emma T"}, // 0.80 over 0.60
		{"Emma", initials, ""},
		{"Emma T", students("Emma T.", "Emma R."), "Emma T."},
		{"Emma Rose", middles, "Emma Rose"},
		{"Emma Roze", middles, "Emma Rose"}, // 0.88 over Emma Louise 0.60
		// `Emma Rows` sits exactly on the margin (0.75 over 0.60) and is
		// left out on purpose: it would pin float noise, not a rule.
		{"Emma Louisa", middles, "Emma Louise"},
		{"Rose", middles, "Emma Rose"},
		{"Emma", middles, ""},
		// A plain `Emma` typed beside `Emma Rose` takes the bare label.
		{"Emma", students("Emma Rose", "Emma"), "Emma"},
		{"Rose", students("Emma Rose", "Emma"), "Emma Rose"},
		// Known gap: an initial written as a word. Voxtral did not do this
		// in the probe; pinned so a fix shows up deliberately.
		{"Emma Tee", initials, ""}, // 0.71 over 0.57, margin 0.14
	})
}

// TestMatchStudent_NumberedRosterFixture: the numbered_roster eval fixture's
// Go half. Voxtral writes a spoken `Arthur one` as `Arthur 1` (probe,
// 2026-09-03), which is how the fixture's transcript carries it; it must
// reach the child, as must a mangled stem.
func TestMatchStudent_NumberedRosterFixture(t *testing.T) {
	classes := fixtureClasses(t, "numbered_roster")
	require.Len(t, classes, 1)
	class := classes[0].Students
	runMatchCases(t, []matchCase{
		{"Arthur 2", class, "Arthur 2"},
		{"Arthur 1", class, "Arthur 1"},
		{"Artur 2", class, "Arthur 2"},
		{"Arthur", class, ""},
	})
}

func fixtureClasses(t *testing.T, fixture string) []ClassGroup {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("evals", "fixtures", "extraction", fixture, "classes.json"))
	require.NoError(t, err)
	var classes []ClassGroup
	require.NoError(t, json.Unmarshal(raw, &classes))
	return classes
}

// TestMatchStudent_FullNameRosterFixture: the extraction eval carries a
// full-name roster again in fixtures/extraction/full_name_roster, so the
// harness covers #111. It needs a model run to grade; this pins the Go half
// without one — every expected child resolves from their first name within
// their class — so a roster edit cannot quietly turn the fixture red.
func TestMatchStudent_FullNameRosterFixture(t *testing.T) {
	classes := fixtureClasses(t, "full_name_roster")

	var expected struct {
		Students []struct {
			Name      string `json:"name"`
			ClassName string `json:"class_name"`
		} `json:"expected_students"`
	}
	raw, err := os.ReadFile(filepath.Join("evals", "fixtures", "extraction", "full_name_roster", "expected.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &expected))
	require.NotEmpty(t, expected.Students)

	for _, exp := range expected.Students {
		parts := strings.Fields(exp.Name)
		require.Greater(t, len(parts), 1, "%q is not a full name", exp.Name)
		var class []ClassStudent
		for _, c := range classes {
			if c.Name == exp.ClassName {
				class = c.Students
			}
		}
		require.NotEmpty(t, class, "class %q not in classes.json", exp.ClassName)
		got, ok := MatchStudent(parts[0], class)
		assert.True(t, ok, "first name %q", parts[0])
		assert.Equal(t, exp.Name, got)
	}
}
