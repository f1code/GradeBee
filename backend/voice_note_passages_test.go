package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func child(label, student, summary string) ExtractedPassage {
	return ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{label}, Student: student, Summary: summary}
}

// A shared observation comes back once per child, each copy with its own
// student, so a pair reaches both of them. The single-call extractor could not
// do this: it returned one entry per child and folded the pair into whichever
// one it named first.
func TestAssemblePassages_PairReachesBothChildren(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		child("Zachariah", "Zachariah", "Zachariah did very well."),
		child("Anaya", "Anaya", "Anaya did very well."),
	})

	assert.Equal(t, []assembledNote{
		{Name: "Zachariah", Summary: "Zachariah did very well.", Passages: 1},
		{Name: "Anaya", Summary: "Anaya did very well.", Passages: 1},
	}, notes)
	assert.Len(t, passages, 2)
}

// Several passages about one child are one note, in the order the teacher
// spoke them, with a blank line between: they are separate stretches of speech,
// and running them together invents a sentence nobody said.
func TestAssemblePassages_OneNotePerChildInSpokenOrder(t *testing.T) {
	notes, _ := assemblePassages([]ExtractedPassage{
		child("Rémi", "Rémi", "He was a little active."),
		child("Capucine", "Capucine", "She read her book."),
		child("Rémi", "Rémi", "He settled by the end."),
	})

	require.Len(t, notes, 2)
	assert.Equal(t, "Rémi", notes[0].Name, "notes follow first mention, not roster order")
	assert.Equal(t, "He was a little active.\n\nHe settled by the end.", notes[0].Summary)
	assert.Equal(t, 2, notes[0].Passages)
	assert.Equal(t, "Capucine", notes[1].Name)
}

// A class-wide statement reaches every child this recording named and nobody
// else. A child who was never mentioned was absent or not discussed, and a note
// about a day they may not have been there for is worse than no note.
func TestAssemblePassages_GroupReachesOnlyTheChildrenNamed(t *testing.T) {
	notes, _ := assemblePassages([]ExtractedPassage{
		{Kind: PassageGroup, Summary: "We practised the date all hour."},
		child("Théo", "Théo", "He read well."),
		child("Lina", "Lina", "She was quiet."),
	})

	require.Len(t, notes, 2)
	for _, n := range notes {
		assert.Contains(t, n.Summary, "We practised the date all hour.")
		assert.Equal(t, 2, n.Passages)
	}
	// Spoken first, but it belongs to the hour rather than to the sentence it
	// preceded, so it closes each note.
	assert.Equal(t, "He read well.\n\nWe practised the date all hour.", notes[0].Summary)
}

func TestAssemblePassages_GroupAloneCreatesNoNote(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		{Kind: PassageGroup, Summary: "Everyone worked hard."},
	})

	assert.Empty(t, notes, "a class-wide statement is not a note for a class nobody was named in")
	assert.Len(t, passages, 1)
	// And the card must not offer a class pick over it. No name was spoken, so
	// no class the teacher chooses can resolve anything — the picker would be a
	// button that cannot work.
	assert.Equal(t, NoNotesNobodyNamed, noNotesReason(len(notes), passages))
}

// The header is dropped before the card sees it. Otherwise a recording holding
// nothing but a header would have one passage and no note, which is exactly the
// state that offers the teacher a class picker — over a passage there is
// nothing to pick for.
func TestAssemblePassages_NoneIsDroppedFromTheCard(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		{Kind: PassageNone, Summary: "Marta, Wednesday, quarter to six."},
	})

	assert.Empty(t, notes)
	assert.Empty(t, passages)
	assert.Equal(t, NoNotesNobodyNamed, noNotesReason(len(notes), passages))
}

// Both ways a passage about one child reaches none of them. Neither becomes a
// note; both stay on the card, because the class picker is what rescues them.
func TestAssemblePassages_UnattributedReachesNobodyButStaysOnTheCard(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		child("Polly", "", "She knocked on the boxes."),
		{Kind: PassageUnknown, Summary: "And then she stopped."},
	})

	assert.Empty(t, notes)
	require.Len(t, passages, 2)
	// The spoken label survives on the one that has one: that is what the
	// picker re-resolves against the class the teacher chooses.
	assert.Equal(t, []string{"Polly"}, passages[0].SpokenLabels)
	assert.Empty(t, passages[1].SpokenLabels)
	assert.Equal(t, NoNotesNoNameMatched, noNotesReason(len(notes), passages))
}

func TestCountKinds(t *testing.T) {
	counts := countKinds([]ExtractedPassage{
		child("A", "A", "x"),
		child("B", "B", "y"),
		{Kind: PassageUnknown},
		{Kind: PassageGroup},
		{Kind: PassageNone},
		{Kind: PassageNone},
	})

	assert.Equal(t, map[PassageKind]int{
		PassageChild: 2, PassageUnknown: 1, PassageGroup: 1, PassageNone: 2,
	}, counts)
}
