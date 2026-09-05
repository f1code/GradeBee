package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The transcript these tests assemble: a header, then one observation each for
// the two children on the Tuesday roster.
const assembleTranscript = "Monday morning. Alice did great. Bob was loud."

// assembleWorld is one teacher with two classes — the sibling pair this flow
// exists for — and one recording whose transcript is on the row.
//
// Alice and Bob are on Tuesday. Monday holds two other children, so a recording
// the extractor files to Monday resolves nobody and the teacher's way out is to
// pick Tuesday.
type assembleWorld struct {
	classRepo   *ClassRepo
	studentRepo *StudentRepo
	noteRepo    *NoteRepo
	voiceNotes  *VoiceNoteRepo
	queue       *stubVoiceNoteQueue
	uploadID    int64
	alice, bob  int64
	monday      string
	tuesday     string
	deps        *mockDepsAll
}

func newAssembleWorld(t *testing.T) *assembleWorld {
	t.Helper()
	ctx := context.Background()
	db := setupTestDB(t)
	w := &assembleWorld{
		classRepo:   &ClassRepo{db: db},
		studentRepo: &StudentRepo{db: db},
		noteRepo:    &NoteRepo{db: db},
		voiceNotes:  &VoiceNoteRepo{db: db},
		queue:       newStubVoiceNoteQueue(),
	}

	tuesday := newTestClass(t, w.classRepo, "test-group", "u1", "Tuesday", "")
	w.tuesday = tuesday.Name
	alice, err := w.studentRepo.Create(ctx, tuesday.ID, "Alice")
	require.NoError(t, err)
	bob, err := w.studentRepo.Create(ctx, tuesday.ID, "Bob")
	require.NoError(t, err)
	w.alice, w.bob = alice.ID, bob.ID

	monday := newTestClass(t, w.classRepo, "test-group", "u1", "Monday", "")
	w.monday = monday.Name
	_, err = w.studentRepo.Create(ctx, monday.ID, "Zephyrine")
	require.NoError(t, err)
	_, err = w.studentRepo.Create(ctx, monday.ID, "Ozymandias")
	require.NoError(t, err)

	vn, err := w.voiceNotes.Create(ctx, "u1", "monday.m4a", "/nowhere/monday.m4a")
	require.NoError(t, err)
	w.uploadID = vn.ID
	require.NoError(t, w.voiceNotes.SetTranscript(ctx, w.uploadID, assembleTranscript))

	roster := newDBRoster(w.classRepo, w.studentRepo, "u1")
	w.deps = &mockDepsAll{
		db:             db,
		classRepo:      w.classRepo,
		studentRepo:    w.studentRepo,
		noteRepo:       w.noteRepo,
		voiceNoteRepo:  w.voiceNotes,
		voiceNoteQueue: w.queue,
		roster:         roster,
		extractor:      &stubExtractor{},
		noteCreator:    newDBNoteCreator(w.noteRepo),
	}
	withDeps(t, w.deps)
	return w
}

// post drives the assemble route through the real router, as user, and returns
// the recorder plus everything the handler logged.
func (w *assembleWorld) post(t *testing.T, user string, uploadID int64, body any) (rec *httptest.ResponseRecorder, logs string) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/voice-notes/%d/assemble", uploadID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	ctx, buf := captureLogs(req.Context())
	rec = httptest.NewRecorder()
	newAPIMux(fakeAuth(user, "test-group", "org:member")).ServeHTTP(rec, req.WithContext(ctx))
	return rec, buf.String()
}

func (w *assembleWorld) notesFor(t *testing.T, studentID int64) []Note {
	t.Helper()
	notes, err := w.noteRepo.List(context.Background(), studentID)
	require.NoError(t, err)
	return notes
}

// declinedPassages is what the card holds after a run that made no note: one
// passage per mention, each carrying the label the extraction model wrote and
// the text a note would have held.
func declinedPassages(labels ...string) []JobPassage {
	if labels == nil {
		labels = []string{"Alice", "Bob"}
	}
	out := make([]JobPassage, len(labels))
	for i, l := range labels {
		out[i] = JobPassage{
			Kind:         PassageChild,
			SpokenLabels: []string{l},
			Summary:      l + "'s passage",
		}
	}
	return out
}

func (w *assembleWorld) doneJob(t *testing.T, passages []JobPassage) {
	t.Helper()
	require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
		UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a",
		Status: JobStatusDone, Passages: passages,
		NoNotesReason: NoNotesNoNameMatched,
	}))
}

// The case this endpoint exists for. The extractor is forced to name one of the
// teacher's classes, so a Tuesday recording lands on Monday, every name misses
// Monday's roster and nobody gets a note. The teacher picks Tuesday and the
// notes the recording was always going to make exist.
//
// The labels are canonical against the roster the model was shown — the wrong
// one — so this also pins that MatchStudent re-resolves them against the picked
// class. If it did not, the picker would create nothing and the flow would be
// decorative.
func TestAssembleNotes_RescuesARecordingFiledToTheSiblingClass(t *testing.T) {
	w := newAssembleWorld(t)
	w.doneJob(t, declinedPassages())

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, w.tuesday, resp.ClassName)
	assert.Empty(t, resp.NoNotesReason)
	require.Len(t, resp.NoteLinks, 2)
	assert.Equal(t, "Alice", resp.NoteLinks[0].Name)
	assert.Equal(t, "Bob", resp.NoteLinks[1].Name)

	// The passages come back saying who each one reached, so the card can
	// stop offering the picker.
	require.Len(t, resp.Passages, 2)
	assert.Equal(t, "Alice", resp.Passages[0].Student)
	assert.Equal(t, "Bob", resp.Passages[1].Student)

	alice := w.notesFor(t, w.alice)
	require.Len(t, alice, 1)
	assert.Equal(t, "Alice's passage", alice[0].Summary)
	// The teacher supplied the who, the model wrote the text.
	assert.Equal(t, NoteSourceReviewed, alice[0].Source)
	require.NotNil(t, alice[0].Transcript)
	assert.Equal(t, assembleTranscript, *alice[0].Transcript)
	assert.Len(t, w.notesFor(t, w.bob), 1)
}

// The note's date is the day the teacher recorded, read off the voice_notes
// row. notes.date is a bare TEXT column the report query compares with BETWEEN,
// so a note dated any other way is invisible to every report.
func TestAssembleNotes_DatesNotesFromTheRecording(t *testing.T) {
	w := newAssembleWorld(t)
	w.doneJob(t, declinedPassages())

	row, err := w.voiceNotes.GetByID(context.Background(), w.uploadID)
	require.NoError(t, err)
	want := row.CreatedAt[:len(time.DateOnly)]

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, want, w.notesFor(t, w.alice)[0].Date)
}

// Picking the wrong one of two sibling classes is the mistake this path exists
// to undo, so a pick that resolved nobody must leave the job untouched and the
// teacher able to pick again.
func TestAssembleNotes_APickThatMadeNoNoteCanBeRetried(t *testing.T) {
	w := newAssembleWorld(t)
	w.doneJob(t, declinedPassages())

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: w.monday, Passages: declinedPassages(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.NoteLinks)
	assert.Equal(t, NoNotesNoNameMatched, resp.NoNotesReason)

	// The job keeps no class: none is filed against this recording.
	job, err := w.queue.GetJob(context.Background(), voiceNoteKey("u1", w.uploadID))
	require.NoError(t, err)
	assert.Empty(t, job.ClassName)

	// And the right class still works.
	rec, _ = w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// A successful pick updates the queued job, so the card's next poll agrees with
// what was created — and the second call is refused rather than doubling the
// notes.
func TestAssembleNotes_UpdatesQueuedJobThenRefusesASecondCall(t *testing.T) {
	w := newAssembleWorld(t)
	w.doneJob(t, declinedPassages())
	body := AssembleNotesRequest{ClassName: w.tuesday, Passages: declinedPassages()}

	rec, _ := w.post(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	job, err := w.queue.GetJob(context.Background(), voiceNoteKey("u1", w.uploadID))
	require.NoError(t, err)
	assert.Equal(t, w.tuesday, job.ClassName)
	assert.Len(t, job.NoteLinks, 2)
	assert.Empty(t, job.NoNotesReason)

	rec, _ = w.post(t, "u1", w.uploadID, body)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// A job mid-run would have the processor create its own notes for the same
// children seconds later; a failed one's route is retry, not a class.
func TestAssembleNotes_RefusesAJobThatIsNotDone(t *testing.T) {
	for _, status := range []string{JobStatusQueued, JobStatusTranscribing, JobStatusExtracting, JobStatusCreatingNotes, JobStatusFailed} {
		t.Run(status, func(t *testing.T) {
			w := newAssembleWorld(t)
			require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
				UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a", Status: status,
			}))
			rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
				ClassName: w.tuesday, Passages: declinedPassages(),
			})
			assert.Equal(t, http.StatusConflict, rec.Code)
			assert.Empty(t, w.notesFor(t, w.alice))
		})
	}
}

// A card still open in a tab after a restart has no job behind it. The notes
// must still be creatable: the transcript is on the row and that is all
// assembly needs.
func TestAssembleNotes_WorksWithNoJobInTheQueue(t *testing.T) {
	w := newAssembleWorld(t)
	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// The recovery record reaches Sentry, so it may carry no name and no text
// (docs/adr/0003).
func TestAssembleNotes_RecoveryRecordOmitsNameAndText(t *testing.T) {
	w := newAssembleWorld(t)
	w.doneJob(t, declinedPassages())

	rec, logs := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, "process voice note: passage recovered")
	assert.NotContains(t, logs, "Alice", "no student name")
	// The summary is what a note holds; every passage's ends in this string,
	// which carries no name of its own, so the assertion fails on the text
	// rather than on the name the line above already covers.
	assert.NotContains(t, logs, "'s passage", "no note text")
}

// A class the caller does not own is a 404, and so is another teacher's
// recording — the same body either way, so probing tells the caller nothing.
func TestAssembleNotes_RefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	w := newAssembleWorld(t)
	w.doneJob(t, declinedPassages())

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{
		ClassName: "Someone else's class", Passages: declinedPassages(),
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec, _ = w.post(t, "u2", w.uploadID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, w.notesFor(t, w.alice))
}

// A row the retention cleanup emptied, or a job that failed before
// transcription: there is nothing to file a note against.
func TestAssembleNotes_RefusesARowWithNoTranscript(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	vn, err := w.voiceNotes.Create(ctx, "u1", "silent.m4a", "/nowhere/silent.m4a")
	require.NoError(t, err)

	rec, _ := w.post(t, "u1", vn.ID, AssembleNotesRequest{
		ClassName: w.tuesday, Passages: declinedPassages(),
	})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAssembleNotes_RequiresAClassAndPassages(t *testing.T) {
	w := newAssembleWorld(t)
	for _, tc := range []struct {
		name string
		body any
	}{
		{"no class", AssembleNotesRequest{Passages: declinedPassages()}},
		{"no passages", AssembleNotesRequest{ClassName: w.tuesday}},
		{"not json", "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := w.post(t, "u1", w.uploadID, tc.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// One child named in two passages gets one note holding both, not two notes.
// The single-call extractor returns one passage per child, so this is what
// stops a group passage — the shape #125 adds — landing a note per passage.
func TestAssembleNotes_GroupsPassagesByChild(t *testing.T) {
	w := newAssembleWorld(t)
	passages := []JobPassage{
		{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Summary: "first"},
		{Kind: PassageChild, SpokenLabels: []string{"Alice", "Bob"}, Summary: "second"},
	}
	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday, Passages: passages})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	alice := w.notesFor(t, w.alice)
	require.Len(t, alice, 1)
	assert.Equal(t, "first\n\nsecond", alice[0].Summary)
	require.Len(t, w.notesFor(t, w.bob), 1)
}

func TestNoNotesReason(t *testing.T) {
	assert.Empty(t, noNotesReason(1, 3))
	assert.Equal(t, NoNotesNobodyNamed, noNotesReason(0, 0))
	assert.Equal(t, NoNotesNoNameMatched, noNotesReason(0, 3))
}

func TestUploadDay(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"2026-03-04T09:15:00.000Z", "2026-03-04", true},
		{"2026-03-04", "2026-03-04", true},
		{"not-a-date-at-all", "", false},
		{"short", "", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := uploadDay(tc.in)
			if !tc.ok {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
