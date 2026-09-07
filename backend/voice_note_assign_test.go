package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows a done card holds for note 694's shape: two blocks the recording
// never named anybody in, and one class-wide remark. Every summary is shorter
// than assembleTranscript, which is the only text check the endpoint makes.
var (
	helping  = AssignPassage{Kind: PassageUnknown, Summary: "helped the little ones"}
	quiet    = AssignPassage{Kind: PassageUnknown, Summary: "did not say much"}
	everyone = AssignPassage{Kind: PassageGroup, Summary: "everyone worked hard"}
)

// assign drives the assign route through the real router, as user, and
// returns the recorder plus everything the handler logged.
func (w *assembleWorld) assign(t *testing.T, user string, uploadID int64, body any) (rec *httptest.ResponseRecorder, logs string) {
	t.Helper()
	ctx, buf := captureLogs(context.Background())
	rec = w.serveAssign(t, ctx, user, uploadID, body)
	return rec, buf.String()
}

func (w *assembleWorld) serveAssign(t *testing.T, ctx context.Context, user string, uploadID int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if s, ok := body.(string); ok {
		raw = []byte(s)
	} else {
		var err error
		raw, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/voice-notes/%d/assign", uploadID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newAPIMux(fakeAuth(user, "test-group", "org:member")).ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// toAlice is the body the card sends after ticking rows and picking Alice on
// the Tuesday roster.
func (w *assembleWorld) toAlice(passages ...AssignPassage) AssignPassagesRequest {
	return AssignPassagesRequest{ClassID: w.tuesdayID, StudentID: w.alice, Passages: passages}
}

// gatedNoteCreator holds every CreateNote open until gate is closed. It is
// how a test keeps the upload lock taken while a second request arrives.
type gatedNoteCreator struct {
	inner NoteCreator
	gate  chan struct{}
	// entered is closed once the first call is inside, so the test can
	// send the second request only after the lock is held.
	entered chan struct{}
}

func (g *gatedNoteCreator) CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error) {
	select {
	case <-g.entered:
	default:
		close(g.entered)
	}
	<-g.gate
	return g.inner.CreateNote(ctx, req)
}

// pipelineNoteFor is the note the pipeline made for a child on this recording,
// holding the child's passage and the class-wide remark, on the queued job as
// a link. It is what an append lands on. It carries the row's trace id, as
// the pipeline stamps it, which is what the duplicate guard keys on; the
// date is the row's day, though the guard no longer reads it.
func (w *assembleWorld) pipelineNoteFor(t *testing.T, studentID int64, name string) Note {
	t.Helper()
	ctx := context.Background()
	row, err := w.voiceNotes.GetByID(ctx, w.uploadID)
	require.NoError(t, err)
	day, err := uploadDay(row.CreatedAt)
	require.NoError(t, err)
	res, err := w.deps.noteCreator.CreateNote(ctx, CreateNoteRequest{
		StudentID: studentID, StudentName: name,
		QuotedText: name + " finished the puzzle alone\n\neveryone worked hard",
		Transcript: assembleTranscript, Date: day, ModelVersion: "pipeline-model",
		TraceID: row.TraceID,
	})
	require.NoError(t, err)
	require.NoError(t, w.queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a", Status: JobStatusDone,
		ClassName: w.tuesday, ClassID: w.tuesdayID,
		NoteLinks: []NoteLink{{Name: name, NoteID: res.NoteID, StudentID: studentID, ClassName: w.tuesday}},
	}))
	note, err := w.noteRepo.GetByID(ctx, res.NoteID)
	require.NoError(t, err)
	return note
}

// The case this endpoint exists for. Note 694's shape: a block the recording
// only ever said "she" about, which the teacher knows was Alice. Ticked, filed,
// and a note exists — dated from the recording, holding the passage plus the
// class-wide remark, stamped with the extractor's model under a source of its
// own.
func TestAssignPassages_FilesARowToAChildAsANewNote(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping, everyone))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Alice", resp.Name)
	assert.Equal(t, w.alice, resp.StudentID)
	assert.Equal(t, w.tuesday, resp.ClassName)
	assert.False(t, resp.Appended)

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, resp.NoteID, notes[0].ID)
	assert.Equal(t, "helped the little ones\n\neveryone worked hard", notes[0].Summary,
		"the passage, then the class-wide remark, a blank line between — assemblePassages' own join")
	// Not model-written: the words came over the wire from the card, and the
	// server never saw the model produce them. An edit to this note must not
	// fire the implicit thumbs-down against extraction.
	assert.Equal(t, NoteSourceAssigned, notes[0].Source)
	assert.False(t, isModelWritten(notes[0].Source))
	require.NotNil(t, notes[0].ModelVersion)
	assert.Equal(t, w.extractor.Model(), *notes[0].ModelVersion, "the extractor produced the words; it only missed the who")
	require.NotNil(t, notes[0].Transcript)
	assert.Equal(t, assembleTranscript, *notes[0].Transcript)

	row, err := w.voiceNotes.GetByID(context.Background(), w.uploadID)
	require.NoError(t, err)
	assert.Equal(t, row.CreatedAt[:len(time.DateOnly)], notes[0].Date, "dated from the recording, as assemble dates its notes")
	require.NotNil(t, notes[0].TraceID)
	assert.Equal(t, row.TraceID, *notes[0].TraceID, "the note names its recording")

	assert.Empty(t, w.notesFor(t, w.bob))
	assert.Empty(t, w.extractor.calls(), "no model call: the text is the card's")
}

// Lévy's case (#135): the pipeline made a note for the child, and a row the
// recording only said "she" about was Lévy too. Ticked and filed to Lévy, the
// row joins that note after a blank line. One note, not two; the group text
// is not written twice; the note stays the pipeline's — source, model, and
// the absence of a feedback row all say so; and the job is left alone, since
// the link is already on it.
func TestAssignPassages_AppendsToTheChildsExistingNote(t *testing.T) {
	w := newAssembleWorld(t)
	before := w.pipelineNoteFor(t, w.alice, "Alice")

	body := w.toAlice(helping, everyone)
	body.AppendToNoteID = before.ID
	rec, logs := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Appended)
	assert.Equal(t, before.ID, resp.NoteID, "the response names the note the card already holds")
	assert.Equal(t, "Alice", resp.Name)

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	after := notes[0]
	assert.Equal(t, before.Summary+"\n\nhelped the little ones", after.Summary,
		"the row after a blank line; the class-wide remark is already in the note and is not repeated")
	assert.Equal(t, NoteSourceAuto, after.Source, "an append keeps the note's source")
	assert.Equal(t, before.ModelVersion, after.ModelVersion, "an append does not restamp the model")
	assert.Equal(t, before.Date, after.Date)

	fb, err := w.feedback.ListByArtifact(context.Background(), "note", before.ID)
	require.NoError(t, err)
	assert.Empty(t, fb, "filing is not correcting: no implicit thumbs-down")

	job := w.job(t)
	assert.Len(t, job.NoteLinks, 1, "the link was already on the job; an append writes nothing to it")

	assert.Contains(t, logs, `"appended":true`)
	assert.Contains(t, logs, `"passage_count":1`, "what the note gained, not what the card sent")
	assert.Contains(t, logs, `"kind_counts":{"group":1,"unknown":1}`)
}

// Two confirms to the same child in one tab: the first makes the note, the
// card holds its link, and the second sends that id, so the second row lands
// on the first note.
func TestAssignPassages_ASecondAssignToTheSameChildAppends(t *testing.T) {
	w := newAssembleWorld(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping, everyone))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var first AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.False(t, first.Appended)

	body := w.toAlice(quiet, everyone)
	body.AppendToNoteID = first.NoteID
	rec, _ = w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var second AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.True(t, second.Appended)
	assert.Equal(t, first.NoteID, second.NoteID)

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, "helped the little ones\n\neveryone worked hard\n\ndid not say much", notes[0].Summary)
	assert.Equal(t, NoteSourceAssigned, notes[0].Source)
}

// The note named must be the picked child's. Another child's note, or an id
// that is nobody's, is a 404 and nothing is written: not to that note, and
// no new note for the child either. So is the child's own note from another
// recording, or one typed by hand: the replay guard reads this recording's
// notes only, so an append it cannot see would be written twice on a retry.
func TestAssignPassages_RefusesANoteThatIsNotTheChilds(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	bobs := w.pipelineNoteFor(t, w.bob, "Bob")
	other, err := w.voiceNotes.Create(ctx, "u1", "monday-again.m4a", "/nowhere/monday-again.m4a")
	require.NoError(t, err)
	elsewhere := &Note{StudentID: w.alice, Date: bobs.Date, Source: NoteSourceAuto, TraceID: &other.TraceID, Summary: "Alice sang"}
	require.NoError(t, w.noteRepo.Create(ctx, elsewhere))
	typed := &Note{StudentID: w.alice, Date: bobs.Date, Source: NoteSourceManual, Summary: "Alice hummed"}
	require.NoError(t, w.noteRepo.Create(ctx, typed))

	for name, noteID := range map[string]int64{
		"another child's note":                    bobs.ID,
		"a note that does not exist":              9999,
		"the child's note from another recording": elsewhere.ID,
		"a note typed by hand":                    typed.ID,
	} {
		t.Run(name, func(t *testing.T) {
			body := w.toAlice(helping)
			body.AppendToNoteID = noteID
			rec, _ := w.assign(t, "u1", w.uploadID, body)
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		})
	}
	for _, n := range []*Note{elsewhere, typed} {
		after, err := w.noteRepo.GetByID(ctx, n.ID)
		require.NoError(t, err)
		assert.Equal(t, n.Summary, after.Summary, "nothing appended")
	}
	assert.Len(t, w.notesFor(t, w.alice), 2, "no new note for the child")
	after, err := w.noteRepo.GetByID(ctx, bobs.ID)
	require.NoError(t, err)
	assert.Equal(t, bobs.Summary, after.Summary)
}

// A split pronoun run: two blocks about the same child, ticked together, make
// one note with a blank line between them, in the order sent.
func TestAssignPassages_TwoRowsMakeOneNote(t *testing.T) {
	w := newAssembleWorld(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping, quiet))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, "helped the little ones\n\ndid not say much", notes[0].Summary)
}

// Group passages go last whatever order the card sent them in; the server
// owns that rule, as it does for the pipeline's notes.
func TestAssignPassages_GroupTextGoesLast(t *testing.T) {
	w := newAssembleWorld(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(everyone, helping))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "helped the little ones\n\neveryone worked hard", w.notesFor(t, w.alice)[0].Summary)
}

// Every forgery is a 404 and none of them writes: another teacher's recording,
// a class the caller does not own, a child that does not exist, and a child of
// the caller's own other class named against this class's id — the last one
// is what proves the roster the card showed is the one the note is filed to.
func TestAssignPassages_RefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	zephyrine, err := w.studentRepo.FindByNameAndClass(ctx, "Zephyrine", w.monday, "u1")
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		user string
		body AssignPassagesRequest
	}{
		{"another teacher's recording", "u2", w.toAlice(helping)},
		{"a class the caller does not own", "u1", AssignPassagesRequest{ClassID: 9999, StudentID: w.alice, Passages: []AssignPassage{helping}}},
		{"a child that does not exist", "u1", AssignPassagesRequest{ClassID: w.tuesdayID, StudentID: 9999, Passages: []AssignPassage{helping}}},
		{"a child of another class", "u1", AssignPassagesRequest{ClassID: w.tuesdayID, StudentID: zephyrine, Passages: []AssignPassage{helping}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := w.assign(t, tc.user, w.uploadID, tc.body)
			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		})
	}
	assert.Empty(t, w.notesFor(t, w.alice))
	assert.Empty(t, w.notesFor(t, zephyrine))
}

// A row the retention cleanup emptied, or a job that failed before
// transcription: there is nothing to file a note against, and nothing to
// bound the text by.
func TestAssignPassages_RefusesARowWithNoTranscript(t *testing.T) {
	w := newAssembleWorld(t)
	vn, err := w.voiceNotes.Create(context.Background(), "u1", "silent.m4a", "/nowhere/silent.m4a")
	require.NoError(t, err)

	rec, _ := w.assign(t, "u1", vn.ID, w.toAlice(helping))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, w.notesFor(t, w.alice))
}

// The body is refused, never repaired. Each row here is one rule.
func TestAssignPassages_RefusesABadBody(t *testing.T) {
	w := newAssembleWorld(t)
	tooLong := AssignPassage{Kind: PassageUnknown, Summary: strings.Repeat("x", len(assembleTranscript)+1)}

	for _, tc := range []struct {
		name string
		body any
		code int
	}{
		{"not json", "{", http.StatusBadRequest},
		{"no class", AssignPassagesRequest{StudentID: w.alice, Passages: []AssignPassage{helping}}, http.StatusBadRequest},
		{"no student", AssignPassagesRequest{ClassID: w.tuesdayID, Passages: []AssignPassage{helping}}, http.StatusBadRequest},
		{"no passages", w.toAlice(), http.StatusBadRequest},
		{"only group passages", w.toAlice(everyone), http.StatusBadRequest},
		{"only group passages, as an append", func() AssignPassagesRequest {
			b := w.toAlice(everyone)
			b.AppendToNoteID = 1
			return b
		}(), http.StatusBadRequest},
		{"a negative note id", func() AssignPassagesRequest {
			b := w.toAlice(helping)
			b.AppendToNoteID = -1
			return b
		}(), http.StatusBadRequest},
		{"a none passage", w.toAlice(AssignPassage{Kind: PassageNone, Summary: "Monday morning"}), http.StatusBadRequest},
		{"an unknown kind", w.toAlice(AssignPassage{Kind: "header", Summary: "Monday morning"}), http.StatusBadRequest},
		{"an empty summary", w.toAlice(AssignPassage{Kind: PassageUnknown, Summary: "  "}), http.StatusBadRequest},
		{"a summary longer than the recording", w.toAlice(tooLong), http.StatusBadRequest},
		{"an oversize body", `{"classId":1,"studentId":1,"passages":[{"kind":"unknown","summary":"` +
			strings.Repeat("x", maxTextSize+2048) + `"}]}`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := w.assign(t, "u1", w.uploadID, tc.body)
			assert.Equal(t, tc.code, rec.Code, rec.Body.String())
		})
	}
	assert.Empty(t, w.notesFor(t, w.alice))
}

// A double-click: two confirms land at once, and the child must get one note.
// The second is told no at once, not queued behind the first.
func TestAssignPassages_DoubleSubmitCreatesOneNote(t *testing.T) {
	w := newAssembleWorld(t)
	gate := &gatedNoteCreator{inner: w.deps.noteCreator, gate: make(chan struct{}), entered: make(chan struct{})}
	w.deps.noteCreator = gate

	body := w.toAlice(helping)
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- w.serveAssign(t, context.Background(), "u1", w.uploadID, body) }()

	// Wait until the first request is inside the write, holding the lock.
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the first request never reached the note write")
	}

	second := w.serveAssign(t, context.Background(), "u1", w.uploadID, body)
	assert.Equal(t, http.StatusConflict, second.Code, second.Body.String())
	assert.Contains(t, second.Body.String(), "already being filed")

	close(gate.gate)
	require.Equal(t, http.StatusOK, (<-first).Code)
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// The link goes on the queued job, so a reload rebuilds the card with the
// right count and the class picker down — and a later pick is refused rather
// than making a second note for a child who has one. Nothing else on the job
// moves: the passages, the class and the reason are as they were.
func TestAssignPassages_WritesTheLinkToTheQueuedJob(t *testing.T) {
	w := newAssembleWorld(t)
	w.declinedJob(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	job := w.job(t)
	require.Len(t, job.NoteLinks, 1)
	assert.Equal(t, NoteLink{Name: "Alice", NoteID: w.notesFor(t, w.alice)[0].ID, StudentID: w.alice, ClassName: w.tuesday}, job.NoteLinks[0])
	assert.Empty(t, job.ClassName, "the job's class is not the assign call's to set")
	assert.Empty(t, job.Passages)
	assert.Equal(t, NoNotesClassUnclear, job.NoNotesReason)

	rec, _ = w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already has notes")
	assert.Len(t, w.notesFor(t, w.alice), 1)
	assert.Empty(t, w.notesFor(t, w.bob))
}

// A job mid-run would have the pipeline's closing write replace the link list
// seconds later, and a failed one's retry rebuilds the job the same way: the
// note would outlive its link on the card. Neither is reachable from the card,
// which offers these controls on a done card only; this is the API's own line.
// "Already has notes" is deliberately not among these — see the create test,
// where Lévy is filed and two rows are open.
func TestAssignPassages_RefusesAJobThatIsNotDone(t *testing.T) {
	for _, status := range []string{JobStatusQueued, JobStatusTranscribing, JobStatusExtracting, JobStatusCreatingNotes, JobStatusFailed} {
		t.Run(status, func(t *testing.T) {
			w := newAssembleWorld(t)
			require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
				UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a", Status: status,
			}))
			rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping))
			assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			assert.Empty(t, w.notesFor(t, w.alice))
			assert.Empty(t, w.job(t).NoteLinks, "a refusal writes nothing to the job")
		})
	}
}

// A card still open in a tab after a restart has no job behind it. The note
// is still made: the endpoint reads only the row and the request, and the job
// write is skipped.
func TestAssignPassages_WorksWithNoJobInTheQueue(t *testing.T) {
	w := newAssembleWorld(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
	_, err := w.queue.GetJob(context.Background(), voiceNoteKey("u1", w.uploadID))
	assert.Error(t, err, "nothing was written to a queue that held nothing")
}

// The recovery record reaches Sentry, so it may carry no name and no text
// (docs/adr/0003). It shares the pipeline's and the class picker's query
// string, with a route of its own and the per-kind breakdown.
func TestAssignPassages_RecoveryRecordOmitsNameAndText(t *testing.T) {
	w := newAssembleWorld(t)

	rec, logs := w.assign(t, "u1", w.uploadID, w.toAlice(helping, quiet, everyone))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Contains(t, logs, "process voice note: passage recovered")
	assert.Contains(t, logs, `"route":"manual"`)
	assert.Contains(t, logs, `"appended":false`)
	assert.Contains(t, logs, `"duplicate":false`)
	assert.Contains(t, logs, `"passage_count":3`)
	assert.Contains(t, logs, `"kind_counts":{"group":1,"unknown":2}`)
	assert.Contains(t, logs, fmt.Sprintf(`"student_id":%d`, w.alice))
	assert.Contains(t, logs, `"model":"stub-model"`)
	assert.Contains(t, logs, promptHashAttr)
	assert.NotContains(t, logs, "Alice", "no student name")
	assert.NotContains(t, logs, "little ones", "no note text")
	assert.NotContains(t, logs, "worked hard", "no note text")
}

// A replay of a finished call: the response was lost and the card retried,
// or a second tab confirmed the same rows. The child gets one note. The
// second call answers with the first call's link, writes nothing, and says
// so in the log; the job holds one link, not two.
func TestAssignPassages_ReplayMakesNoSecondNote(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	body := w.toAlice(helping, everyone)

	rec, _ := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var first AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))

	rec, logs := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var second AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.Equal(t, first, second, "the same link, in the shape the card already holds")

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, first.NoteID, notes[0].ID)
	assert.Len(t, w.job(t).NoteLinks, 1, "a hit writes nothing to the job")

	assert.Contains(t, logs, "process voice note: passage recovered")
	assert.Contains(t, logs, `"duplicate":true`)
	assert.NotContains(t, logs, "Alice", "no student name")
	assert.NotContains(t, logs, "little ones", "no note text")
}

// A replay of an append: the response was lost after the row joined the
// pipeline's note, and the card retried with the same body, note id and all.
// The guard finds the grown note and answers as the lost response would
// have: that note, `appended: true`, and the row is not written twice.
func TestAssignPassages_ReplayOfAnAppendSaysAppended(t *testing.T) {
	w := newAssembleWorld(t)
	before := w.pipelineNoteFor(t, w.alice, "Alice")
	body := w.toAlice(helping, everyone)
	body.AppendToNoteID = before.ID

	rec, _ := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var first AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	require.True(t, first.Appended)

	rec, logs := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var replay AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replay))
	assert.Equal(t, first, replay, "the same link, appended, as the lost response said")

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, before.Summary+"\n\nhelped the little ones", notes[0].Summary, "the row once, not twice")
	assert.Contains(t, logs, `"duplicate":true`)
	assert.Contains(t, logs, `"appended":true`)
	assert.Contains(t, logs, `"passage_count":1`, "the lost call's count: group text is not counted on an append")
}

// The guard is by text. Once the teacher has rewritten the note, the words
// the card sent are no longer in it, and a second note is fair: the server
// cannot tell a replay from a fresh filing of the same rows, and it must not
// swallow the teacher's edit by answering with the rewritten note.
func TestAssignPassages_ReplayAfterAnEditMakesASecondNote(t *testing.T) {
	w := newAssembleWorld(t)
	body := w.toAlice(helping)

	rec, _ := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	first := w.notesFor(t, w.alice)[0]
	require.NoError(t, w.noteRepo.Update(context.Background(), first.ID, "helped the older ones"))

	rec, logs := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, `"duplicate":false`)
	assert.Len(t, w.notesFor(t, w.alice), 2)
}

// Partial overlap is not a replay. A row already filed plus one more is a
// new filing, and the note is made as normal: the guard asks for every sent
// passage, not any.
func TestAssignPassages_OneExtraRowIsNotAReplay(t *testing.T) {
	w := newAssembleWorld(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec, logs := w.assign(t, "u1", w.uploadID, w.toAlice(helping, quiet))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, `"duplicate":false`)
	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 2)
}

// The guard matches the block this call would write, not the words one by
// one. A note that holds every sent string scattered — the pipeline's own
// note for the same child, drawn from the same transcript and carrying the
// group text already — is not this call's note, and the filing goes ahead.
func TestAssignPassages_ScatteredWordsAreNotAReplay(t *testing.T) {
	w := newAssembleWorld(t)
	row, err := w.voiceNotes.GetByID(context.Background(), w.uploadID)
	require.NoError(t, err)
	day, err := uploadDay(row.CreatedAt)
	require.NoError(t, err)
	require.NoError(t, w.noteRepo.Create(context.Background(), &Note{
		StudentID: w.alice, Date: day, Source: NoteSourceAuto, TraceID: &row.TraceID,
		Summary: "helped the little ones\n\nsang loudly\n\ndid not say much\n\neveryone worked hard",
	}))

	rec, logs := w.assign(t, "u1", w.uploadID, w.toAlice(helping, quiet, everyone))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, `"duplicate":false`)
	assert.Len(t, w.notesFor(t, w.alice), 2)
}

// The other defect the day scope had: the pipeline dates a note by job
// dispatch, assign by the row's creation, and across a UTC midnight those
// are different days. A replay of an append onto such a note then missed and
// made a second note. Keyed on the recording, the day of the note is nothing
// to the guard: the replay hits the note it grew.
func TestAssignPassages_ReplayOfAnAppendHitsAcrossAUTCMidnight(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	row, err := w.voiceNotes.GetByID(ctx, w.uploadID)
	require.NoError(t, err)
	rowDay, err := time.Parse(time.DateOnly, row.CreatedAt[:len(time.DateOnly)])
	require.NoError(t, err)
	dayBefore := rowDay.AddDate(0, 0, -1).Format(time.DateOnly)
	pipelineNote := &Note{
		StudentID: w.alice, Date: dayBefore, Source: NoteSourceAuto, TraceID: &row.TraceID,
		Summary: "Alice finished the puzzle alone\n\neveryone worked hard",
	}
	require.NoError(t, w.noteRepo.Create(ctx, pipelineNote))
	require.NoError(t, w.queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: w.uploadID, TraceID: row.TraceID, FileName: "monday.m4a", Status: JobStatusDone,
		ClassName: w.tuesday, ClassID: w.tuesdayID,
		NoteLinks: []NoteLink{{Name: "Alice", NoteID: pipelineNote.ID, StudentID: w.alice, ClassName: w.tuesday}},
	}))
	body := w.toAlice(helping, everyone)
	body.AppendToNoteID = pipelineNote.ID

	rec, _ := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec, logs := w.assign(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var replay AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replay))
	assert.True(t, replay.Appended)
	assert.Equal(t, pipelineNote.ID, replay.NoteID)
	assert.Contains(t, logs, `"duplicate":true`)

	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1, "no second note: the day of the note is nothing to the guard")
	assert.Equal(t, dayBefore, notes[0].Date)
	assert.Equal(t, pipelineNote.Summary+"\n\nhelped the little ones", notes[0].Summary, "the row once, not twice")
}

// The guard's scope is the recording, not the day. Another recording on the
// same day made a note for the same child holding the same words, and so did
// the teacher by hand; a filing from this recording is not their replay, and
// the child gets a third note. A day scope answered this with the other
// recording's note (#136). The replay of that filing then hits its own note.
func TestAssignPassages_ANoteFromAnotherRecordingIsNotAReplay(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	row, err := w.voiceNotes.GetByID(ctx, w.uploadID)
	require.NoError(t, err)
	day, err := uploadDay(row.CreatedAt)
	require.NoError(t, err)
	other, err := w.voiceNotes.Create(ctx, "u1", "monday-again.m4a", "/nowhere/monday-again.m4a")
	require.NoError(t, err)
	require.NoError(t, w.noteRepo.Create(ctx, &Note{
		StudentID: w.alice, Date: day, Source: NoteSourceAssigned, TraceID: &other.TraceID,
		Summary: "helped the little ones\n\neveryone worked hard",
	}))
	require.NoError(t, w.noteRepo.Create(ctx, &Note{
		StudentID: w.alice, Date: day, Source: NoteSourceManual,
		Summary: "helped the little ones\n\neveryone worked hard",
	}))

	rec, logs := w.assign(t, "u1", w.uploadID, w.toAlice(helping, everyone))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, `"duplicate":false`)
	assert.Contains(t, logs, `"trace_id":"`+row.TraceID+`"`)
	assert.Len(t, w.notesFor(t, w.alice), 3)

	rec, logs = w.assign(t, "u1", w.uploadID, w.toAlice(helping, everyone))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, `"duplicate":true`)
	assert.Len(t, w.notesFor(t, w.alice), 3)
}

// Two notes hold the block (A filed, then A and B, then A replayed). The
// replay answers with the first, the one that call made.
func TestAssignPassages_ReplayPicksTheEarliestMatch(t *testing.T) {
	w := newAssembleWorld(t)

	rec, _ := w.assign(t, "u1", w.uploadID, w.toAlice(helping))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var first AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	rec, _ = w.assign(t, "u1", w.uploadID, w.toAlice(helping, quiet))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec, logs := w.assign(t, "u1", w.uploadID, w.toAlice(helping))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var replay AssignPassagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replay))
	assert.Contains(t, logs, `"duplicate":true`)
	assert.Equal(t, first.NoteID, replay.NoteID)
	assert.Len(t, w.notesFor(t, w.alice), 2)
}
