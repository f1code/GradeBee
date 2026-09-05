// llm_live_test.go holds every test that calls a real LLM provider. Each
// skips itself via requireLiveLLM when the active provider's API key is
// unset, so `make test` is safe without credentials and exercises them
// automatically when a key is present.
//
// These are the smallest live checks that the two-pass contract holds end to
// end. Quality is graded, not gated: the eval harness (backend/evals) scores
// the fixtures on the shipped model, and piling wider shapes in here only
// grows the assertion surface of a test whose answer is a model's.
package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireLiveLLM skips the test if the active LLM provider's API key is unset.
// It returns the configured provider for live tests that need it.
func requireLiveLLM(t *testing.T) LLMProvider {
	t.Helper()
	p, err := LoadProvider()
	if err != nil {
		t.Skipf("LLM provider not configured: %v", err)
	}
	return p
}

// newTestLLMExtractor creates an LLM extractor, skipping if the active provider's API key is not set.
func newTestLLMExtractor(t *testing.T) Extractor {
	t.Helper()
	return newLLMExtractor(requireLiveLLM(t))
}

// contains is a readability helper for the live extraction assertions.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// noteOf joins every passage that reached one child, the way assemblePassages
// does, so a live assertion reads the text that child's note would hold.
func noteOf(t *testing.T, result *ExtractResponse, name string) string {
	t.Helper()
	notes, _ := assemblePassages(result.Passages)
	for _, n := range notes {
		if n.Name == name {
			return n.Summary
		}
	}
	t.Fatalf("%s reached no note; passages: %+v", name, result.Passages)
	return ""
}

// notedChildren names every child the recording reached.
func notedChildren(result *ExtractResponse) []string {
	notes, _ := assemblePassages(result.Passages)
	names := make([]string, len(notes))
	for i, n := range notes {
		names[i] = n.Name
	}
	return names
}

func TestLLM_PinsTheClassFromTheHeader(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Math 101. Alice Johnson demonstrated excellent problem-solving skills on today's algebra quiz. She scored 95% and helped her classmates understand the quadratic formula.",
		Classes:    classes,
	})
	require.NoError(t, err)

	assert.Equal(t, "Math 101", result.ClassName)
	assert.Equal(t, []string{"Alice Johnson"}, notedChildren(result))
}

// A recording spanning two classes gets one roster, and the children of the
// other fall to unattributed rather than into a wrong child's note. That is the
// deliberate cost of pass 1 pinning one class with no decline; the decline is
// #127. Checked against 22 real recordings: none genuinely spans two classes.
func TestLLM_TwoClassesInOneRecordingKeepsOneRoster(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Bob Smith"}, {Name: "Alice Johnson"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Diana Lee"}, {Name: "Charlie Brown"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Today I observed two students. In Math 101, Bob Smith was very engaged during the fractions lesson and volunteered to solve problems on the board. In Science 202, Diana Lee conducted her chemistry experiment carefully and wrote detailed lab notes.",
		Classes:    classes,
	})
	require.NoError(t, err)

	require.Contains(t, []string{"Math 101", "Science 202"}, result.ClassName)
	pinned, ok := findClass(classes, result.ClassName)
	require.True(t, ok)

	// Whoever got a note is on the pinned class's roster. Nobody is filed under
	// a class the recording did not put them in.
	onRoster := map[string]bool{}
	for _, s := range pinned.Students {
		onRoster[s.Name] = true
	}
	for _, name := range notedChildren(result) {
		assert.True(t, onRoster[name], "%q got a note but is not in the pinned class %q", name, result.ClassName)
	}
}

// A recording about a class the teacher does not have. Pass 1's enum has no ""
// yet, so it pins one anyway (#127 is what lets it decline) — but pass 2's
// student enum is that class's roster, so no child of it can pick up the
// observation.
func TestLLM_ChildOffEveryRosterReachesNobody(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Report card for Tommy Wilson, Art 303. Tommy shows great creativity in his paintings and participates actively in class discussions about art history.",
		Classes:    classes,
	})
	require.NoError(t, err)

	assert.Contains(t, []string{"Math 101", "Science 202"}, result.ClassName, "pass 1 must not invent a class")
	assert.Empty(t, notedChildren(result), "no child of the teacher's own classes was named")
}

// A transcription dropping a leading substring of a name ("Adah" for
// "Ada"). Resolving it is what the roster is in pass 2's prompt for.
func TestLLM_TruncatedNameMatch(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "English 101", Students: []ClassStudent{{Name: "Ada Rodriguez"}, {Name: "Katherine Bennet"}}},
		{Name: "History 201", Students: []ClassStudent{{Name: "Theodore Roosevelt"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "English 101. Adah gave a fantastic presentation on the water cycle today. She answered every follow-up question with confidence.",
		Classes:    classes,
	})
	require.NoError(t, err)

	assert.Equal(t, "English 101", result.ClassName)
	assert.Contains(t, noteOf(t, result, "Ada Rodriguez"), "presentation")
}

// TestExtractPreservesTeacherVoice verifies that a summary keeps the teacher's
// original language and emotion rather than formalising it.
func TestExtractPreservesTeacherVoice(t *testing.T) {
	ext := newTestLLMExtractor(t)

	transcript := `Period 3, Thursday. Maxence was impossibly bad today. I'm ready to choke the living
sh*t out of him. He wouldn't stop talking during the lesson.
Amara was great - very attentive and helpful to other students.`

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{Name: "Period 3", Students: []ClassStudent{{Name: "Maxence"}, {Name: "Amara"}}},
		},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Maxence", "Amara"}, notedChildren(result))

	assert.True(t, contains(noteOf(t, result, "Maxence"), "impossibly bad"),
		"the summary does not preserve the teacher's phrasing: %s", noteOf(t, result, "Maxence"))
}

// TestExtractGroupObservations: a statement about the class as a whole reaches
// every child the recording named, and creates a note for nobody else.
func TestExtractGroupObservations(t *testing.T) {
	ext := newTestLLMExtractor(t)

	transcript := `Period 1. Today the class was way too loud and unfocused. Everyone was talking over each other.
Specific note: Tommy helped me organize the materials, which was great.`

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{Name: "Period 1", Students: []ClassStudent{{Name: "Tommy"}, {Name: "Lisa"}}},
		},
	})
	require.NoError(t, err)

	// Lisa was never named: a class-wide statement is not a reason to write her
	// a note about a day she may not have been there for.
	assert.Equal(t, []string{"Tommy"}, notedChildren(result))

	tommy := noteOf(t, result, "Tommy")
	assert.True(t, contains(tommy, "organize"), "Tommy's note is missing his own observation: %s", tommy)
	assert.True(t,
		contains(tommy, "too loud") || contains(tommy, "unfocused") || contains(tommy, "talking over"),
		"Tommy's note is missing the class-wide observation: %s", tommy)
}

// TestExtractNoCrossStudentBleed verifies that an observation about one named
// child never lands in another named child's note. Production regression:
// "Edgar was doing great, but today Anouk was not doing that good" produced
// the full transcript under both names.
//
// This is deliberately the smallest transcript that shows the bug — two
// children, one assertion each way. Wider shapes are the cross_student_bleed
// eval fixture's job.
func TestExtractNoCrossStudentBleed(t *testing.T) {
	ext := newTestLLMExtractor(t)

	// No collective referent anywhere, so the two halves must not be shared.
	transcript := "Oliver Thursday. Edgar was doing great, but today Anouk was not doing that good."

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{Name: "Oliver · Thu", Students: []ClassStudent{{Name: "Anouk"}, {Name: "Edgar"}}},
		},
	})
	require.NoError(t, err)

	edgar := noteOf(t, result, "Edgar")
	assert.True(t, contains(edgar, "doing great"), "Edgar's note is missing his own observation: %s", edgar)
	assert.NotContains(t, edgar, "Anouk", "Edgar's note leaked Anouk's observation: %s", edgar)

	anouk := noteOf(t, result, "Anouk")
	assert.True(t, contains(anouk, "not doing that good"), "Anouk's note is missing her own observation: %s", anouk)
	assert.NotContains(t, anouk, "Edgar", "Anouk's note leaked Edgar's observation: %s", anouk)
}

// TestExtractPassagesSkipsPass1 covers the entry point the class picker uses:
// the class is the caller's answer, not the model's, and the transcript is read
// against it whatever its spoken header says.
func TestExtractPassagesSkipsPass1(t *testing.T) {
	ext := newTestLLMExtractor(t)

	// The header names Math 101. The caller says otherwise, and wins.
	transcript := "Math 101. Diana Lee wrote detailed lab notes and asked good questions."
	science := ClassGroup{Name: "Science 202", Students: []ClassStudent{{Name: "Diana Lee"}, {Name: "Charlie Brown"}}}

	passages, err := ext.ExtractPassages(t.Context(), transcript, science)
	require.NoError(t, err)

	notes, _ := assemblePassages(passages)
	require.NotEmpty(t, notes, "passages: %+v", passages)
	assert.Equal(t, "Diana Lee", notes[0].Name)
}

// An unnamed pronoun block must not be filed under a listed child. The prompt's
// no-elimination rules took this from 8/10 to 0/10 and the Go guard catches
// what is left; this pins the pair working together on the shipped model.
func TestExtractDoesNotFileAPronounBlockUnderAListedChild(t *testing.T) {
	ext := newTestLLMExtractor(t)

	// Ombeline is on the roster and never named. Nothing may reach her.
	transcript := `Marta Wednesday. So she was knocking on the boxes and putting Bunny up and down,
laughing and giggling the whole time. Rémi was a little active today with the magnets.`

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{Name: "Marta · Wed", Students: []ClassStudent{{Name: "Ombeline"}, {Name: "Capucine"}, {Name: "Rémi"}}},
		},
	})
	require.NoError(t, err)

	assert.NotContains(t, notedChildren(result), "Ombeline", "an unnamed block was filed under a listed child")
	assert.NotContains(t, notedChildren(result), "Capucine", "an unnamed block was filed under a listed child")
	assert.Contains(t, notedChildren(result), "Rémi", "the one named child should still get their note")
	assert.NotContains(t, noteOf(t, result, "Rémi"), "Bunny", "the unowned block reached the named child's note")
}
