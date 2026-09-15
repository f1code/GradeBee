package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func child(label, student, summary string) ExtractedPassage {
	return ExtractedPassage{Kind: PassageChild, SpokenLabels: []string{label}, Student: student, Summary: summary}
}

func absentChild(name, summary string) ExtractedPassage {
	return ExtractedPassage{Kind: PassageAbsent, SpokenLabels: []string{name}, Student: name, Summary: summary}
}

// rosterOf is a roster whose ids are 1, 2, 3… in the order given, so a test
// can assert which row a note was filed to.
func rosterOf(names ...string) []ClassStudent {
	out := make([]ClassStudent, len(names))
	for i, n := range names {
		out[i] = ClassStudent{ID: int64(i + 1), Name: n}
	}
	return out
}

// A shared observation comes back once per child, each copy with its own
// student, so a pair reaches both of them. The single-call extractor could not
// do this: it returned one entry per child and folded the pair into whichever
// one it named first.
func TestAssemblePassages_PairReachesBothChildren(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		child("Zachariah", "Zachariah", "Zachariah did very well."),
		child("Anaya", "Anaya", "Anaya did very well."),
	}, rosterOf("Anaya", "Zachariah"))

	assert.Equal(t, []assembledNote{
		{StudentID: 2, Name: "Zachariah", Summary: "Zachariah did very well.", Passages: 1},
		{StudentID: 1, Name: "Anaya", Summary: "Anaya did very well.", Passages: 1},
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
	}, rosterOf("Capucine", "Rémi"))

	require.Len(t, notes, 2)
	assert.Equal(t, "Rémi", notes[0].Name, "notes follow first mention, not roster order")
	assert.Equal(t, int64(2), notes[0].StudentID)
	assert.Equal(t, "He was a little active.\n\nHe settled by the end.", notes[0].Summary)
	assert.Equal(t, 2, notes[0].Passages)
	assert.Equal(t, "Capucine", notes[1].Name)
	assert.Equal(t, int64(1), notes[1].StudentID)
}

// A class-wide statement reaches every child on the roster. A child the teacher
// never mentioned was there for it, so they get a note holding only the group
// text, after the children named, in roster order.
func TestAssemblePassages_GroupReachesTheWholeRoster(t *testing.T) {
	notes, _ := assemblePassages([]ExtractedPassage{
		{Kind: PassageGroup, Summary: "We practised the date all hour."},
		child("Lina", "Lina", "She was quiet."),
		child("Théo", "Théo", "He read well."),
	}, rosterOf("Noor", "Théo", "Ada", "Lina"))

	assert.Equal(t, []assembledNote{
		// Spoken first, but the group text belongs to the hour rather than to
		// the sentence it preceded, so it closes each note.
		{StudentID: 4, Name: "Lina", Summary: "She was quiet.\n\nWe practised the date all hour.", Passages: 2},
		{StudentID: 2, Name: "Théo", Summary: "He read well.\n\nWe practised the date all hour.", Passages: 2},
		{StudentID: 1, Name: "Noor", Summary: "We practised the date all hour.", Passages: 1},
		{StudentID: 3, Name: "Ada", Summary: "We practised the date all hour.", Passages: 1},
	}, notes)
}

// A recording naming nobody still fans out: silence is presence, so a
// group-only recording is a note for every child, and the card has no reason
// to offer a class pick.
func TestAssemblePassages_GroupAloneReachesTheWholeRoster(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		{Kind: PassageGroup, Summary: "Everyone worked hard."},
	}, rosterOf("Ada", "Bo"))

	assert.Equal(t, []assembledNote{
		{StudentID: 1, Name: "Ada", Summary: "Everyone worked hard.", Passages: 1},
		{StudentID: 2, Name: "Bo", Summary: "Everyone worked hard.", Passages: 1},
	}, notes)
	assert.Len(t, passages, 1)
	assert.Empty(t, noNotesReason(len(notes), passages))
}

// A child the teacher named absent keeps their own note and nothing else, even
// with an observation of their own beside the absence.
func TestAssemblePassages_AbsentChildSkipsTheGroup(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		absentChild("Théo", "Théo was absent today."),
		child("Théo", "Théo", "He sent his homework in."),
		child("Camille", "Camille", "Camille worked hard on the letter sounds."),
		{Kind: PassageGroup, Summary: "Everyone loved the song."},
	}, rosterOf("Théo", "Camille", "Noor"))

	assert.Equal(t, []assembledNote{
		{StudentID: 1, Name: "Théo", Summary: "Théo was absent today.\n\nHe sent his homework in.", Passages: 2},
		{StudentID: 2, Name: "Camille", Summary: "Camille worked hard on the letter sounds.\n\nEveryone loved the song.", Passages: 2},
		{StudentID: 3, Name: "Noor", Summary: "Everyone loved the song.", Passages: 1},
	}, notes)
	assert.Len(t, passages, 4)
}

// An absence names a child of this class, so the class is right and the group
// text fans out, even when no other child was named.
func TestAssemblePassages_AbsenceAloneCountsAsResolving(t *testing.T) {
	notes, _ := assemblePassages([]ExtractedPassage{
		absentChild("Théo", "Théo wasn't in today."),
		{Kind: PassageGroup, Summary: "Everyone else did really well."},
	}, rosterOf("Théo", "Noor"))

	assert.Equal(t, []assembledNote{
		{StudentID: 1, Name: "Théo", Summary: "Théo wasn't in today.", Passages: 1},
		{StudentID: 2, Name: "Noor", Summary: "Everyone else did really well.", Passages: 1},
	}, notes)
}

// Names spoken and none on the roster: the recording was read against the
// wrong class. Fanning out would write notes for that whole roster and close
// the class picker, so nothing reaches anybody and the reason stays the one
// that offers the picker.
func TestAssemblePassages_SpokenNamesNoneMatchedSuppressesTheGroup(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		child("Zephyrine", "", "Zephyrine drew the farm."),
		{Kind: PassageGroup, Summary: "All the kids loved the animal game."},
	}, rosterOf("Alice", "Bob"))

	assert.Empty(t, notes)
	assert.Len(t, passages, 2)
	assert.Equal(t, NoNotesNoNameMatched, noNotesReason(len(notes), passages))
	assert.True(t, canPickClass(noNotesReason(len(notes), passages)))
}

// Known limit, pinned so it is found rather than rediscovered. The model may
// still return a name that fits nobody as kind unknown with no labels
// (offRosterAsUnknown in voice_note_assemble_test.go). Then no name was
// spoken as far as the fold can tell, so a wrong-class recording fans out to
// the whole wrong roster and noNotesReason no longer offers the picker.
func TestAssemblePassages_OffRosterNameAsUnknownIsNotSuppressed(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		{Kind: PassageUnknown, Summary: "Zephyrine drew the farm."},
		{Kind: PassageGroup, Summary: "All the kids loved the animal game."},
	}, rosterOf("Alice", "Bob"))

	assert.Len(t, notes, 2)
	assert.Empty(t, noNotesReason(len(notes), passages))
}

// Without a group passage the roster changes nothing: children never mentioned
// get no note.
func TestAssemblePassages_NoGroupPassageLeavesTheRosterAlone(t *testing.T) {
	notes, _ := assemblePassages([]ExtractedPassage{
		child("Théo", "Théo", "He read well."),
		{Kind: PassageUnknown, Summary: "And then she stopped."},
	}, rosterOf("Théo", "Noor", "Ada"))

	assert.Equal(t, []assembledNote{
		{StudentID: 1, Name: "Théo", Summary: "He read well.", Passages: 1},
	}, notes)
}

// The header is dropped before the card sees it. Otherwise a recording holding
// nothing but a header would have one passage and no note, which is exactly the
// state that offers the teacher a class picker — over a passage there is
// nothing to pick for.
func TestAssemblePassages_NoneIsDroppedFromTheCard(t *testing.T) {
	notes, passages := assemblePassages([]ExtractedPassage{
		{Kind: PassageNone, Summary: "Marta, Wednesday, quarter to six."},
	}, rosterOf("Ada"))

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
	}, rosterOf("Ada"))

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
		{Kind: PassageAbsent, Student: "C"},
		{Kind: PassageUnknown},
		{Kind: PassageGroup},
		{Kind: PassageNone},
		{Kind: PassageNone},
	})

	assert.Equal(t, map[PassageKind]int{
		PassageChild: 2, PassageAbsent: 1, PassageUnknown: 1, PassageGroup: 1, PassageNone: 2,
	}, counts)
}
