package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// undo drives the undo route through the real router, as user, and returns
// the recorder plus everything the handler logged.
func (w *assembleWorld) undo(t *testing.T, user string, uploadID, studentID int64) (rec *httptest.ResponseRecorder, logs string) {
	t.Helper()
	ctx, buf := captureLogs(context.Background())
	req := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/voice-notes/%d/assign/%d", uploadID, studentID), http.NoBody)
	rec = httptest.NewRecorder()
	newAPIMux(fakeAuth(user, "test-group", "org:member")).ServeHTTP(rec, req.WithContext(ctx))
	return rec, buf.String()
}

// assigned files passages to Alice and returns the link the call made.
func (w *assembleWorld) assigned(t *testing.T, body AssignPassagesRequest) AssignPassagesResponse {
	t.Helper()
	rec, _ := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// The honest mistake. A row filed to Alice that was Bob's: undo takes the
// note and its link off the job, and the same row then files to Bob as a
// note of its own — one for Bob, none for Alice.
func TestUndoAssignment_RemovesTheNoteAndItsLink(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	made := w.assigned(t, w.toAlice(helping, everyone))
	require.Len(t, w.job(t).NoteLinks, 1)

	rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp UndoAssignmentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []int64{made.NoteID}, resp.NoteIDs)

	assert.Empty(t, w.notesFor(t, w.alice))
	assert.Empty(t, w.job(t).NoteLinks, "the link goes with the note")
	fb, err := w.feedback.ListByArtifact(context.Background(), "note", made.NoteID)
	require.NoError(t, err)
	assert.Empty(t, fb, "assigned is not model-written: no thumbs-down")

	w.assigned(t, AssignPassagesRequest{ClassID: w.tuesdayID, StudentID: w.bob, Passages: []AssignPassage{helping, everyone}})
	assert.Len(t, w.notesFor(t, w.bob), 1)
	assert.Empty(t, w.notesFor(t, w.alice))
	assert.Len(t, w.job(t).NoteLinks, 1)
}

// A second row filed to the same child joined the first row's note (#135).
// The undo is of the assignment, not a row: the whole note goes, and the card
// reopens every row that was on it.
func TestUndoAssignment_TakesAppendedRowsWithIt(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	made := w.assigned(t, w.toAlice(helping, everyone))
	body := w.toAlice(quiet)
	body.AppendToNoteID = made.NoteID
	grown := w.assigned(t, body)
	require.True(t, grown.Appended)

	rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, w.notesFor(t, w.alice))
	assert.Empty(t, w.job(t).NoteLinks)
}

// Rows appended to a note the pipeline made (#135) are not an assignment
// the server can take back: the note's source is the pipeline's, and most of
// its text is too. 404, and the note keeps every word.
func TestUndoAssignment_RefusesANoteThatIsNotAssigned(t *testing.T) {
	w := newAssembleWorld(t)
	pipeline := w.pipelineNoteFor(t, w.alice, "Alice")
	body := w.toAlice(quiet)
	body.AppendToNoteID = pipeline.ID
	w.assigned(t, body)

	rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, pipeline.Summary+"\n\n"+quiet.Summary, notes[0].Summary)
	assert.Equal(t, NoteSourceAuto, notes[0].Source)
	assert.Len(t, w.job(t).NoteLinks, 1)
}

// Nothing was assigned to this child from this recording: nothing to undo.
func TestUndoAssignment_RefusesAChildWithNoAssignment(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	w.assigned(t, w.toAlice(helping))

	rec, _ := w.undo(t, "u1", w.uploadID, w.bob)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1, "Alice's assignment is not Bob's to undo")
	assert.Len(t, w.job(t).NoteLinks, 1)
}

// Bare 404 on every gate, and nothing deleted: another teacher's recording,
// and a child who is not the caller's — even one with an assigned note from
// this very recording, planted here to prove the ownership gate runs before
// the note lookup.
func TestUndoAssignment_RefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	w.misfiledJob(t)
	w.assigned(t, w.toAlice(helping))

	theirs := newTestClass(t, w.classRepo, "test-group", "u2", "Thursday", "")
	yolanda, err := w.studentRepo.Create(ctx, theirs.ID, "Yolanda")
	require.NoError(t, err)
	row, err := w.voiceNotes.GetByID(ctx, w.uploadID)
	require.NoError(t, err)
	require.NoError(t, w.noteRepo.Create(ctx, &Note{
		StudentID: yolanda.ID, Date: "2026-03-26", Source: NoteSourceAssigned, TraceID: &row.TraceID,
		Summary: "planted",
	}))

	for _, tc := range []struct {
		name    string
		user    string
		student int64
	}{
		{"another teacher's recording", "u2", w.alice},
		{"another teacher's child", "u1", yolanda.ID},
		{"a child that does not exist", "u1", 9999},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := w.undo(t, tc.user, w.uploadID, tc.student)
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		})
	}
	assert.Len(t, w.notesFor(t, w.alice), 1)
	assert.Len(t, w.notesFor(t, yolanda.ID), 1)
	assert.Len(t, w.job(t).NoteLinks, 1)
}

// The scope is this recording, by trace id. An assigned note the same child
// got from another recording is not touched, whatever its day.
func TestUndoAssignment_LeavesOtherRecordingsAlone(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	w.misfiledJob(t)
	other, err := w.voiceNotes.Create(ctx, "u1", "monday-again.m4a", "/nowhere/monday-again.m4a")
	require.NoError(t, err)
	require.NoError(t, w.noteRepo.Create(ctx, &Note{
		StudentID: w.alice, Date: "2026-03-26", Source: NoteSourceAssigned, TraceID: &other.TraceID,
		Summary: "helped the little ones",
	}))
	made := w.assigned(t, w.toAlice(helping))

	rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp UndoAssignmentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []int64{made.NoteID}, resp.NoteIDs)

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, other.TraceID, *notes[0].TraceID)
}

// A card still open after a restart has no job behind it. The note still
// goes: the endpoint reads the row and the notes, and skips the job write.
func TestUndoAssignment_WorksWithNoJobInTheQueue(t *testing.T) {
	w := newAssembleWorld(t)
	w.assigned(t, w.toAlice(helping))

	rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, w.notesFor(t, w.alice))
	_, err := w.queue.GetJob(context.Background(), voiceNoteKey("u1", w.uploadID))
	assert.Error(t, err, "nothing was written to a queue that held nothing")
}

// The same line assign draws: a job mid-run or failed is not a card the
// teacher can undo from, and the link list is about to be rewritten anyway.
func TestUndoAssignment_RefusesAJobThatIsNotDone(t *testing.T) {
	for _, status := range []string{JobStatusQueued, JobStatusTranscribing, JobStatusExtracting, JobStatusCreatingNotes, JobStatusFailed} {
		t.Run(status, func(t *testing.T) {
			w := newAssembleWorld(t)
			made := w.assigned(t, w.toAlice(helping))
			require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
				UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a", Status: status,
				NoteLinks: []NoteLink{{Name: "Alice", NoteID: made.NoteID, StudentID: w.alice}},
			}))
			rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
			assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			assert.Len(t, w.notesFor(t, w.alice), 1)
			assert.Len(t, w.job(t).NoteLinks, 1, "a refusal writes nothing to the job")
		})
	}
}

// A second undo mid-flight, or an assign racing an undo, gets the lock's 409.
func TestUndoAssignment_RefusesWhileTheRecordingIsLocked(t *testing.T) {
	w := newAssembleWorld(t)
	w.assigned(t, w.toAlice(helping))
	key := voiceNoteKey("u1", w.uploadID)
	require.True(t, takeUploadLock(key))
	defer releaseUploadLock(key)

	rec, _ := w.undo(t, "u1", w.uploadID, w.alice)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// The record reaches Sentry, so no name and no text (docs/adr/0003).
func TestUndoAssignment_RecordOmitsNameAndText(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	w.assigned(t, w.toAlice(helping, everyone))
	row, err := w.voiceNotes.GetByID(context.Background(), w.uploadID)
	require.NoError(t, err)

	rec, logs := w.undo(t, "u1", w.uploadID, w.alice)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Contains(t, logs, "process voice note: assignment undone")
	assert.Contains(t, logs, `"route":"manual"`)
	assert.Contains(t, logs, `"note_count":1`)
	assert.Contains(t, logs, fmt.Sprintf(`"student_id":%d`, w.alice))
	assert.Contains(t, logs, fmt.Sprintf(`"upload_id":%d`, w.uploadID))
	assert.Contains(t, logs, `"trace_id":"`+row.TraceID+`"`)
	assert.NotContains(t, logs, "Alice", "no student name")
	assert.NotContains(t, logs, "little ones", "no note text")
	assert.NotContains(t, logs, "worked hard", "no note text")
}
