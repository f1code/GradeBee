package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// the extraction filed to Monday resolves nobody and the teacher's way out is
// to pick Tuesday.
type assembleWorld struct {
	classRepo   *ClassRepo
	studentRepo *StudentRepo
	noteRepo    *NoteRepo
	feedback    *ArtifactFeedbackRepo
	voiceNotes  *VoiceNoteRepo
	queue       *stubVoiceNoteQueue
	extractor   *stubExtractor
	uploadID    int64
	alice, bob  int64
	monday      string
	tuesday     string
	mondayID    int64
	tuesdayID   int64
	deps        *mockDepsAll
}

// rosterPass2 is what pass 2 answers with: one passage per name the recording
// spoke, read against whichever class it was handed. A name on that class
// becomes a child passage; a name that is not becomes a passage owned by
// nobody.
//
// The shipped prompt admits two shapes for a name that is on no listed child,
// and this returns the one that keeps a spoken label: kind "child" with
// student "" — "for a 'child' passage, the listed child's name exactly as
// listed below, or ” when no listed child fits" (prompts_version.go). It is
// the shape that makes noNotesReason answer no_name_matched, which is the card
// the class picker is offered on.
//
// The other shape is offRosterAsUnknown below. Both are real; a test that only
// knows one of them is testing its own stub.
func rosterPass2(labels ...string) func(ClassGroup) []ExtractedPassage {
	if labels == nil {
		labels = []string{"Alice", "Bob"}
	}
	return func(class ClassGroup) []ExtractedPassage {
		out := make([]ExtractedPassage, len(labels))
		for i, l := range labels {
			p := ExtractedPassage{
				Kind:         PassageChild,
				SpokenLabels: []string{l},
				Summary:      l + "'s passage",
			}
			for _, st := range class.Students {
				if st.Name == l {
					p.Student = l
				}
			}
			out[i] = p
		}
		return out
	}
}

// offRosterAsUnknown is the same recording under the prompt's other reading of
// a name that fits nobody: kind "unknown", and an EMPTY spoken_labels, because
// the prompt says "a name that matches nobody listed" is unknown and "Empty
// list for 'unknown', 'group' and 'none'".
//
// It is the dangerous shape. Those passages carry no spoken name, so
// noNotesReason answers nobody_named — the same answer a recording that truly
// named nobody produces. A handler that treated that reason as terminal would
// take the picker off the wrong-sibling-class recording it exists for.
func offRosterAsUnknown(labels ...string) func(ClassGroup) []ExtractedPassage {
	inner := rosterPass2(labels...)
	return func(class ClassGroup) []ExtractedPassage {
		out := inner(class)
		for i, p := range out {
			if p.Kind == PassageChild && p.Student == "" {
				out[i] = ExtractedPassage{Kind: PassageUnknown, Summary: p.Summary}
			}
		}
		return out
	}
}

func newAssembleWorld(t *testing.T) *assembleWorld {
	t.Helper()
	ctx := context.Background()
	db := setupTestDB(t)

	// The lock map is package-global and every world here is upload 1 for u1, so
	// they all share one key. A test that fails before releasing it would 409
	// every test after it — a cascade that says nothing about the failure.
	uploadLocksMu.Lock()
	uploadLocks = map[string]struct{}{}
	uploadLocksMu.Unlock()

	w := &assembleWorld{
		classRepo:   &ClassRepo{db: db},
		studentRepo: &StudentRepo{db: db},
		noteRepo:    &NoteRepo{db: db},
		feedback:    &ArtifactFeedbackRepo{db: db},
		voiceNotes:  &VoiceNoteRepo{db: db},
		queue:       newStubVoiceNoteQueue(),
		extractor:   &stubExtractor{passagesFn: rosterPass2()},
	}

	tuesday := newTestClass(t, w.classRepo, "test-group", "u1", "Tuesday", "")
	w.tuesday, w.tuesdayID = tuesday.Name, tuesday.ID
	alice, err := w.studentRepo.Create(ctx, tuesday.ID, "Alice")
	require.NoError(t, err)
	bob, err := w.studentRepo.Create(ctx, tuesday.ID, "Bob")
	require.NoError(t, err)
	w.alice, w.bob = alice.ID, bob.ID

	monday := newTestClass(t, w.classRepo, "test-group", "u1", "Monday", "")
	w.monday, w.mondayID = monday.Name, monday.ID
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
		feedbackRepo:   w.feedback,
		voiceNoteRepo:  w.voiceNotes,
		voiceNoteQueue: w.queue,
		roster:         roster,
		extractor:      w.extractor,
		noteCreator:    newDBNoteCreator(w.noteRepo),
	}
	withDeps(t, w.deps)
	return w
}

// post drives the assemble route through the real router, as user, and returns
// the recorder plus everything the handler logged. The log is read after
// ServeHTTP returns, so nothing is still writing to it.
func (w *assembleWorld) post(t *testing.T, user string, uploadID int64, body any) (rec *httptest.ResponseRecorder, logs string) {
	t.Helper()
	ctx, buf := captureLogs(context.Background())
	rec = w.serve(t, ctx, user, uploadID, body)
	return rec, buf.String()
}

// serve is post without the log capture, so the goroutines of the
// double-submit test do not write into one buffer at once.
func (w *assembleWorld) serve(t *testing.T, ctx context.Context, user string, uploadID int64, body any) *httptest.ResponseRecorder {
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/voice-notes/%d/assemble", uploadID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newAPIMux(fakeAuth(user, "test-group", "org:member")).ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func (w *assembleWorld) notesFor(t *testing.T, studentID int64) []Note {
	t.Helper()
	notes, err := w.noteRepo.List(context.Background(), studentID)
	require.NoError(t, err)
	return notes
}

func (w *assembleWorld) job(t *testing.T) *VoiceNoteJob {
	t.Helper()
	job, err := w.queue.GetJob(context.Background(), voiceNoteKey("u1", w.uploadID))
	require.NoError(t, err)
	return job
}

// declinedJob is the card a decline leaves: done, no class, no passages, and
// the reason that puts the class picker up (#127).
func (w *assembleWorld) declinedJob(t *testing.T) {
	t.Helper()
	require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
		UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a",
		Status: JobStatusDone, NoNotesReason: NoNotesClassUnclear, CanPickClass: true,
	}))
}

// misfiledJob is the other card that reaches this endpoint: the recording was
// read against the wrong sibling class, so it has passages and every spoken
// name missed that roster.
func (w *assembleWorld) misfiledJob(t *testing.T) {
	t.Helper()
	_, passages := assemblePassages(rosterPass2()(ClassGroup{Name: "Monday"}))
	require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
		UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a",
		Status: JobStatusDone, Passages: passages,
		NoNotesReason: NoNotesNoNameMatched, CanPickClass: true,
	}))
}

// The case this endpoint exists for. A recording the extraction filed to Monday
// resolved nobody; the teacher picks Tuesday, pass 2 runs against Tuesday's
// roster, and the notes the recording was always going to make exist.
func TestAssembleNotes_RescuesARecordingFiledToTheSiblingClass(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Pass 2 ran once, against the class the teacher picked. Getting this wrong
	// would leave the picker resolving against the roster that already failed.
	calls := w.extractor.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, w.tuesday, calls[0].Name)

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, w.tuesday, resp.ClassName)
	assert.Equal(t, w.tuesdayID, resp.ClassID, "the card's student picker needs the row, not the name")
	assert.Empty(t, resp.NoNotesReason)
	assert.False(t, resp.CanPickClass, "the recording is filed; there is nothing left to pick")
	require.Len(t, resp.NoteLinks, 2)
	assert.Equal(t, "Alice", resp.NoteLinks[0].Name)
	assert.Equal(t, "Bob", resp.NoteLinks[1].Name)

	// The passages come back saying who each one reached, so the card can stop
	// offering the picker.
	require.Len(t, resp.Passages, 2)
	assert.Equal(t, "Alice", resp.Passages[0].Student)
	assert.Equal(t, "Bob", resp.Passages[1].Student)

	alice := w.notesFor(t, w.alice)
	require.Len(t, alice, 1)
	assert.Equal(t, "Alice's passage", alice[0].Summary)
	row, err := w.voiceNotes.GetByID(context.Background(), w.uploadID)
	require.NoError(t, err)
	require.NotNil(t, alice[0].TraceID)
	assert.Equal(t, row.TraceID, *alice[0].TraceID, "the note names its recording")
	// Since #127 the model wrote these words in this request, so the source
	// constant is literally true: the teacher supplied only the class.
	assert.Equal(t, NoteSourceReviewed, alice[0].Source)
	require.NotNil(t, alice[0].Transcript)
	assert.Equal(t, assembleTranscript, *alice[0].Transcript)
	assert.Len(t, w.notesFor(t, w.bob), 1)
}

// A declined recording — pass 1 could not pin a class, so pass 2 never ran and
// the card holds no passages at all. The pick is that pass's deferred first
// run, and it must produce the same notes as the misfiled case: one path.
func TestAssembleNotes_RescuesADeclinedRecording(t *testing.T) {
	w := newAssembleWorld(t)
	w.declinedJob(t)

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, w.tuesday, resp.ClassName)
	assert.Empty(t, resp.NoNotesReason)
	require.Len(t, resp.NoteLinks, 2)
	assert.Len(t, w.notesFor(t, w.alice), 1)
	assert.Len(t, w.notesFor(t, w.bob), 1)

	job := w.job(t)
	assert.Equal(t, w.tuesday, job.ClassName)
	assert.Empty(t, job.NoNotesReason)
}

// The note's date is the day the teacher recorded, read off the voice_notes
// row. notes.date is a bare TEXT column the report query compares with BETWEEN,
// so a note dated any other way is invisible to every report.
func TestAssembleNotes_DatesNotesFromTheRecording(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)

	row, err := w.voiceNotes.GetByID(context.Background(), w.uploadID)
	require.NoError(t, err)
	want := row.CreatedAt[:len(time.DateOnly)]

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, want, w.notesFor(t, w.alice)[0].Date)
}

// Picking the wrong one of two sibling classes is the mistake this path exists
// to undo, so a pick that resolved nobody must leave the job untouched and the
// teacher able to pick again.
//
// The response reports the pick — the class and the passages the run returned —
// and the reason stays the job's. A declined recording holds no passages of
// its own, so the run's are the only rows the teacher can file by hand, and
// the picked class is the only roster to file them to; but nothing was filed,
// so the job is not written and the picker stays up. If the pick was the wrong
// sibling, the teacher reads the summaries against the wrong roster and picks
// again.
func TestAssembleNotes_APickThatMadeNoNoteCanBeRetried(t *testing.T) {
	w := newAssembleWorld(t)
	w.declinedJob(t)

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.monday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.NoteLinks)
	assert.Equal(t, w.monday, resp.ClassName, "the pick, so the card can offer its roster")
	assert.Equal(t, w.mondayID, resp.ClassID)
	assert.Len(t, resp.Passages, 2, "the run's rows, the only ones a declined recording has")
	assert.Equal(t, NoNotesClassUnclear, resp.NoNotesReason, "the card's own reason, unchanged")

	assert.True(t, resp.CanPickClass, "the picker stays up for the next attempt")

	job := w.job(t)
	assert.Empty(t, job.ClassName, "nothing was filed, so the job is unwritten")
	assert.Zero(t, job.ClassID)
	assert.Equal(t, NoNotesClassUnclear, job.NoNotesReason)
	assert.Empty(t, job.NoteLinks)

	// And the right class still works.
	rec, _ = w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// A pass 2 that came back with no spoken name anywhere. It reads as
// nobody_named — and that must NOT end the picker, because it is also what the
// wrong roster produces: the prompt calls a name fitting no listed child
// "unknown", and unknown carries an empty spoken_labels. A handler that
// terminated on this reason would strand exactly the recording the picker
// exists for, with the job written and no way back.
//
// This is the shape, and the test below is the case.
func TestAssembleNotes_APickWithNoSpokenNameLeavesTheJobAlone(t *testing.T) {
	w := newAssembleWorld(t)
	w.declinedJob(t)
	w.extractor.passagesFn = func(ClassGroup) []ExtractedPassage {
		return []ExtractedPassage{{Kind: PassageUnknown, Summary: "somebody did well"}}
	}

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.NoteLinks)
	assert.Equal(t, w.tuesday, resp.ClassName, "the pick is reported; the reason is not derived from it")
	assert.Len(t, resp.Passages, 1)
	assert.Equal(t, NoNotesClassUnclear, resp.NoNotesReason, "the card's own reason, unchanged")

	job := w.job(t)
	assert.Empty(t, job.ClassName)
	assert.Empty(t, job.NoteLinks)
	assert.Equal(t, NoNotesClassUnclear, job.NoNotesReason)
	assert.Empty(t, job.Passages)
}

// The case: a declined recording, picked to the wrong one of two sibling
// classes, where pass 2 returned every off-roster name as an unlabelled
// unknown. The teacher must still be able to pick the right class.
//
// Without this the endpoint's own reason for existing is unreachable: the pick
// would write nobody_named, the card would drop the picker (JobStatus.tsx), and
// a declined recording has no earlier passages to fall back on.
func TestAssembleNotes_AWrongRosterPickThatReturnedNoLabelsCanStillBeRetried(t *testing.T) {
	w := newAssembleWorld(t)
	w.declinedJob(t)
	w.extractor.passagesFn = offRosterAsUnknown()

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.monday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.NoteLinks)
	assert.Equal(t, NoNotesClassUnclear, resp.NoNotesReason, "the picker must survive this")

	job := w.job(t)
	assert.Equal(t, NoNotesClassUnclear, job.NoNotesReason)
	assert.Empty(t, job.ClassName)

	// And the right class still works.
	rec, _ = w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
	assert.Len(t, w.notesFor(t, w.bob), 1)
}

// The model call is bounded on its own, not by the handler: a whole-handler
// deadline would cancel the note loop mid-write. 30s, because llmChatTimeout is
// 120s and no teacher waits that long behind a spinner.
func TestAssembleNotes_BoundsPass2WithItsOwnDeadline(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	deadline, ok := w.extractor.lastDeadline()
	require.True(t, ok, "pass 2 ran with no deadline at all")
	assert.InDelta(t, assemblePass2Timeout.Seconds(), time.Until(deadline).Seconds(), 5)
}

// A provider error or timeout must not burn the recording: pass 2 runs before
// the first CreateNote, so nothing is written and the card keeps its picker.
func TestAssembleNotes_AFailedPass2LeavesTheCardInThePickerState(t *testing.T) {
	w := newAssembleWorld(t)
	w.declinedJob(t)
	w.extractor.err = io.ErrUnexpectedEOF

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	assert.Empty(t, w.notesFor(t, w.alice))
	assert.Empty(t, w.notesFor(t, w.bob))
	job := w.job(t)
	assert.Empty(t, job.ClassName)
	assert.Empty(t, job.NoteLinks)
	assert.Equal(t, NoNotesClassUnclear, job.NoNotesReason)

	// And the teacher can retry.
	w.extractor.err = nil
	rec, _ = w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// A double-click, or a client retry: two picks land at once, and every child
// must get one note.
//
// Ordering pass 2 first does not cover this — it moves the model call inside
// the job-read-to-write window rather than shrinking it. The lock is the
// mechanism, and blocking pass 2 is what holds the first request inside it
// while the second arrives.
func TestAssembleNotes_DoubleSubmitCreatesOneSetOfNotes(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	w.extractor.blockPass2 = make(chan struct{})

	body := AssembleNotesRequest{ClassName: w.tuesday}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- w.serve(t, context.Background(), "u1", w.uploadID, body) }()

	// Wait until the first request is inside pass 2, holding the lock.
	require.Eventually(t, func() bool { return len(w.extractor.calls()) == 1 },
		2*time.Second, 5*time.Millisecond)

	second := w.serve(t, context.Background(), "u1", w.uploadID, body)
	assert.Equal(t, http.StatusConflict, second.Code, second.Body.String())

	close(w.extractor.blockPass2)
	require.Equal(t, http.StatusOK, (<-first).Code)

	assert.Len(t, w.extractor.calls(), 1, "the refused request must not have run pass 2")
	assert.Len(t, w.notesFor(t, w.alice), 1)
	assert.Len(t, w.notesFor(t, w.bob), 1)
}

// A successful pick updates the queued job, so the card's next poll agrees with
// what was created — and the second call is refused rather than doubling the
// notes.
func TestAssembleNotes_UpdatesQueuedJobThenRefusesASecondCall(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)
	body := AssembleNotesRequest{ClassName: w.tuesday}

	rec, _ := w.post(t, "u1", w.uploadID, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	job := w.job(t)
	assert.Equal(t, w.tuesday, job.ClassName)
	assert.Equal(t, w.tuesdayID, job.ClassID)
	assert.Len(t, job.NoteLinks, 2)
	assert.Empty(t, job.NoNotesReason)

	rec, _ = w.post(t, "u1", w.uploadID, body)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// A job mid-run would have the processor create its own notes for the same
// children seconds later; a failed one's route is retry, not a class.
//
// A decline is not among these: it completes done, which is what lets the card
// it leaves reach this endpoint at all.
func TestAssembleNotes_RefusesAJobThatIsNotDone(t *testing.T) {
	for _, status := range []string{JobStatusQueued, JobStatusTranscribing, JobStatusExtracting, JobStatusCreatingNotes, JobStatusFailed} {
		t.Run(status, func(t *testing.T) {
			w := newAssembleWorld(t)
			require.NoError(t, w.queue.Publish(context.Background(), VoiceNoteJob{
				UserID: "u1", UploadID: w.uploadID, FileName: "monday.m4a", Status: status,
			}))
			rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
			assert.Equal(t, http.StatusConflict, rec.Code)
			assert.Empty(t, w.notesFor(t, w.alice))
			assert.Empty(t, w.extractor.calls(), "a refusal must not reach the model")
		})
	}
}

// A card still open in a tab after a restart has no job behind it. The notes
// must still be creatable: the transcript is on the row and that is all pass 2
// needs.
func TestAssembleNotes_WorksWithNoJobInTheQueue(t *testing.T) {
	w := newAssembleWorld(t)
	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// The same card, picked to the wrong class. There is no job to mirror, so the
// response is all the teacher gets — and it wins over the poll for the rest of
// that card's life (JobStatus.tsx keeps a forgotten job's done card, and an
// assemble result overrides it). So it may report the pick, but must not name
// a cause from this run's own reading: pass 2 against the wrong roster returns
// an off-roster name as an unlabelled unknown, which reads as nobody_named and
// would take the picker away for good in that tab.
func TestAssembleNotes_WithNoJobAPickThatMadeNothingKeepsThePicker(t *testing.T) {
	w := newAssembleWorld(t)
	w.extractor.passagesFn = offRosterAsUnknown()

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.monday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.NoteLinks)
	assert.Equal(t, w.monday, resp.ClassName, "the pick is reported even with no job to mirror")
	assert.Equal(t, w.mondayID, resp.ClassID)
	assert.Empty(t, resp.NoNotesReason, "the handler cannot know the cause here, so it names none")
	assert.True(t, resp.CanPickClass, "the picker must survive this")
	assert.Empty(t, w.notesFor(t, w.alice))

	// And the right class still works.
	rec, _ = w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, w.notesFor(t, w.alice), 1)
}

// The recovery record reaches Sentry, so it may carry no name and no text
// (docs/adr/0003). The per-kind breakdown rides the completion record beside
// it: this is the second place pass 2 runs, and a breakdown on one path only
// makes the readout lie by omission.
func TestAssembleNotes_RecoveryRecordOmitsNameAndText(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)

	rec, logs := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, logs, "process voice note: passage recovered")
	assert.Contains(t, logs, `"route":"class_picker"`)
	assert.Contains(t, logs, `"passages_child":2`)
	assert.Contains(t, logs, `"passages_total":2`)
	assert.NotContains(t, logs, "Alice", "no student name")
	// The summary is what a note holds; every passage's ends in this string,
	// which carries no name of its own, so the assertion fails on the text
	// rather than on the name the line above already covers.
	assert.NotContains(t, logs, "'s passage", "no note text")
}

// A class the caller does not own is a 404, and so is another teacher's
// recording — the same body either way, so probing tells the caller nothing.
// Neither reaches the model: ownership runs ahead of pass 2, so a probe cannot
// spend a model call or take a lock.
func TestAssembleNotes_RefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	w := newAssembleWorld(t)
	w.misfiledJob(t)

	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: "Someone else's class"})
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec, _ = w.post(t, "u2", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, w.notesFor(t, w.alice))
	assert.Empty(t, w.extractor.calls(), "a 404 must make no model call")
}

// A row the retention cleanup emptied, or a job that failed before
// transcription: there is nothing to file a note against.
func TestAssembleNotes_RefusesARowWithNoTranscript(t *testing.T) {
	w := newAssembleWorld(t)
	ctx := context.Background()
	vn, err := w.voiceNotes.Create(ctx, "u1", "silent.m4a", "/nowhere/silent.m4a")
	require.NoError(t, err)

	rec, _ := w.post(t, "u1", vn.ID, AssembleNotesRequest{ClassName: w.tuesday})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// The class is the whole body since #127. A tab open from before still posts
// its passages; Go's decoder ignores them and the pick works.
func TestAssembleNotes_RequiresAClassAndIgnoresAnythingElse(t *testing.T) {
	w := newAssembleWorld(t)
	for _, tc := range []struct {
		name string
		body any
		code int
	}{
		{"no class", AssembleNotesRequest{}, http.StatusBadRequest},
		{"not json", "{", http.StatusBadRequest},
		{"an old tab's passages", map[string]any{
			"className": w.tuesday,
			"passages":  []JobPassage{{Kind: PassageChild, Summary: "words the model never wrote"}},
		}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := w.post(t, "u1", w.uploadID, tc.body)
			assert.Equal(t, tc.code, rec.Code, rec.Body.String())
		})
	}
	// And what the old tab posted reached no note.
	notes := w.notesFor(t, w.alice)
	require.Len(t, notes, 1)
	assert.Equal(t, "Alice's passage", notes[0].Summary)
}

// One child named in two passages gets one note holding both, not two notes.
// This is assemblePassages' own rule, and the point of the picker calling it:
// the deleted resolvePassages dropped the group passage the pipeline fans out
// to every child, so the two paths made different notes from one recording.
func TestAssembleNotes_FoldsPassagesTheWayThePipelineDoes(t *testing.T) {
	w := newAssembleWorld(t)
	w.extractor.passagesFn = func(ClassGroup) []ExtractedPassage {
		return []ExtractedPassage{
			{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "first"},
			{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "second"},
			{Kind: PassageGroup, Summary: "everyone worked hard"},
		}
	}
	rec, _ := w.post(t, "u1", w.uploadID, AssembleNotesRequest{ClassName: w.tuesday})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	alice := w.notesFor(t, w.alice)
	require.Len(t, alice, 1)
	assert.Equal(t, "first\n\nsecond\n\neveryone worked hard", alice[0].Summary)
	// Bob was never named, so the class-wide remark reaches him not at all.
	assert.Empty(t, w.notesFor(t, w.bob))
}

// assembleOutcome is where a pick decides what the teacher is told, so its
// three outcomes are pinned directly rather than only through the route. The
// class and the passages are the run's in every row; the reason and the gate
// are the job's, or absent-and-open when there is no job.
func TestAssembleOutcome(t *testing.T) {
	ran := AssembleNotesResponse{
		ClassName: "Tuesday",
		ClassID:   3,
		NoteLinks: []NoteLink{{Name: "Alice", NoteID: 1}},
		Passages:  []JobPassage{{Kind: PassageChild, Summary: "x"}},
	}
	job := &VoiceNoteJob{
		ClassName: "", Passages: []JobPassage{{Kind: PassageUnknown, Summary: "y"}},
		NoNotesReason: NoNotesClassUnclear, CanPickClass: true,
	}

	t.Run("notes created", func(t *testing.T) {
		got := assembleOutcome(job, ran)
		assert.Equal(t, ran, got, "what the run produced, verbatim")
		assert.False(t, got.CanPickClass, "the recording is filed")
	})

	t.Run("no notes, job known", func(t *testing.T) {
		empty := ran
		empty.NoteLinks = nil
		got := assembleOutcome(job, empty)
		assert.Empty(t, got.NoteLinks)
		assert.NotNil(t, got.NoteLinks, "[] on the wire, never null")
		assert.Equal(t, "Tuesday", got.ClassName, "the pick, so the card can offer its roster")
		assert.Equal(t, int64(3), got.ClassID)
		assert.Equal(t, ran.Passages, got.Passages, "the run's rows, not the job's")
		assert.Equal(t, NoNotesClassUnclear, got.NoNotesReason, "the job's own reason, unchanged")
		assert.True(t, got.CanPickClass)
	})

	t.Run("no notes, job forgotten", func(t *testing.T) {
		empty := ran
		empty.NoteLinks = nil
		got := assembleOutcome(nil, empty)
		assert.Empty(t, got.NoteLinks)
		assert.Equal(t, "Tuesday", got.ClassName)
		assert.Equal(t, ran.Passages, got.Passages)
		assert.Empty(t, got.NoNotesReason, "the cause is unknowable here, so none is named")
		assert.True(t, got.CanPickClass, "and the teacher can still try")
	})
}

func TestCanPickClass(t *testing.T) {
	assert.True(t, canPickClass(NoNotesClassUnclear))
	assert.True(t, canPickClass(NoNotesNoNameMatched))
	// A recording that named nobody cannot be rescued by a class: the names are
	// read off the transcript, and no pick puts one there.
	assert.False(t, canPickClass(NoNotesNobodyNamed))
	assert.False(t, canPickClass(""), "a recording with notes has nothing to pick")
}

func TestNoNotesReason(t *testing.T) {
	named := []JobPassage{{Kind: PassageChild, SpokenLabels: []string{"Polly"}, Summary: "x"}}
	unnamed := []JobPassage{{Kind: PassageUnknown, Summary: "x"}, {Kind: PassageGroup, Summary: "y"}}

	assert.Empty(t, noNotesReason(1, named))
	assert.Equal(t, NoNotesNobodyNamed, noNotesReason(0, nil))
	assert.Equal(t, NoNotesNoNameMatched, noNotesReason(0, named))
	// The case the passage contract adds: blocks the recording never named
	// anybody in. They are not names that missed the roster, and no class the
	// teacher picks can resolve them — so this must not read as no_name_matched,
	// which is what puts the class picker on the card.
	assert.Equal(t, NoNotesNobodyNamed, noNotesReason(0, unnamed))
	// A mixed recording still offers the pick, for the one name that could land.
	assert.Equal(t, NoNotesNoNameMatched, noNotesReason(0, append(append([]JobPassage{}, unnamed...), named...)))
	// A label the model put on a passage that should carry none, and that is a
	// pronoun rather than a name. MatchStudent stop-lists it, so no class the
	// teacher picks can resolve it — it must not count as a spoken name.
	pronoun := []JobPassage{{Kind: PassageGroup, SpokenLabels: []string{"She"}, Summary: "x"}}
	assert.Equal(t, NoNotesNobodyNamed, noNotesReason(0, pronoun))
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
