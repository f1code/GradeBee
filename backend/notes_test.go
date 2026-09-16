package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A child deleted between the roster read and the insert refuses one note in
// the batch; the notes filed before it roll back with it. Retry then files one
// note per remaining child.
func TestFileNotes_OneRefusedInsertWritesNothing(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	class, err := r.classes.Create(ctx, "g", "u1", testLevelID(t, r.classes.db, "g", "Math"), "Monday", "")
	require.NoError(t, err)
	var notes []assembledNote
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		s, err := r.students.Create(ctx, class.ID, name)
		require.NoError(t, err)
		notes = append(notes, assembledNote{StudentID: s.ID, Name: name, Summary: name + " read well.", Passages: 1})
	}
	require.NoError(t, r.students.Delete(ctx, notes[1].StudentID))

	rec := recording{Date: "2026-09-16", ClassName: class.Name, TraceID: "t1"}
	nc := newDBNoteCreator(r.notes)
	links, err := fileNotes(ctx, nc, rec, notes, NoteSourceAuto)
	require.Error(t, err)
	assert.Nil(t, links)
	var refused *createNotesError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, 1, refused.Index)
	assert.Equal(t, notes[1].StudentID, refused.StudentID)
	assert.NotContains(t, err.Error(), "Bob", "the error names nobody (docs/adr/0003)")
	assert.Equal(t, 0, countNotesFor(t, r, "t1"), "Alice's note must roll back with Bob's")

	links, err = fileNotes(ctx, nc, rec, []assembledNote{notes[0], notes[2]}, NoteSourceAuto)
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, 2, countNotesFor(t, r, "t1"), "one note per child after retry")
	for i, l := range links {
		n, err := r.notes.GetByID(ctx, l.NoteID)
		require.NoError(t, err)
		assert.Equal(t, []assembledNote{notes[0], notes[2]}[i].StudentID, n.StudentID)
	}
}

func countNotesFor(t *testing.T, r *repos, traceID string) int {
	t.Helper()
	var n int
	require.NoError(t, r.notes.db.QueryRow("SELECT count(*) FROM notes WHERE trace_id = ?", traceID).Scan(&n))
	return n
}
