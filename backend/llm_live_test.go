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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
//
// No roster, here and in notedChildren: these assertions are about who the
// recording named, so a group passage reaches only the children named rather
// than the whole class.
func noteOf(t *testing.T, result *ExtractResponse, name string) string {
	t.Helper()
	notes, _ := assemblePassages(result.Passages, nil)
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
	notes, _ := assemblePassages(result.Passages, nil)
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

// A recording spanning two classes reaches no child of the wrong one. Since
// #127 pass 1 may answer "" here — two classes named is one of the two measured
// decline triggers — and either answer is safe: a decline reaches nobody at
// all, and a pin reaches only children of the class it pinned. What must never
// happen is a child of the other class picking up an observation.
//
// Checked against 22 real recordings: none genuinely spans two classes.
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

	if result.ClassName == "" {
		assert.Empty(t, result.Passages, "a decline stops before pass 2")
		return
	}

	pinned, ok := findClass(classes, result.ClassName)
	require.True(t, ok, "pass 1 must not invent a class")

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

// A recording about a class the teacher does not have. Pass 1 either declines
// or pins one of the teacher's own — it can never invent Art 303 — and pass 2's
// student enum is the pinned class's roster, so no child of it can pick up the
// observation either way.
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

	assert.Contains(t, []string{"", "Math 101", "Science 202"}, result.ClassName, "pass 1 must not invent a class")
	assert.Empty(t, notedChildren(result), "no child of the teacher's own classes was named")
}

// A transcription dropping a leading substring of a name ("Malia" for
// "Amalia"). Resolving it is what the roster is in pass 2's prompt for.
func TestLLM_TruncatedNameMatch(t *testing.T) {
	ext := newTestLLMExtractor(t)
	classes := []ClassGroup{
		{Name: "English 101", Students: []ClassStudent{{Name: "Amalia Rodriguez"}, {Name: "Elizabeth Bennet"}}},
		{Name: "History 201", Students: []ClassStudent{{Name: "Theodore Roosevelt"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "English 101. Malia gave a fantastic presentation on the water cycle today. She answered every follow-up question with confidence.",
		Classes:    classes,
	})
	require.NoError(t, err)

	assert.Equal(t, "English 101", result.ClassName)
	assert.Contains(t, noteOf(t, result, "Amalia Rodriguez"), "presentation")
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
// every child the recording named, and creates a note for nobody else. That
// holds only because noteOf folds with no roster; in production the group text
// reaches the whole roster.
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
// "Harry was doing great, but today Dinara was not doing that good" produced
// the full transcript under both names.
//
// This is deliberately the smallest transcript that shows the bug — two
// children, one assertion each way. Wider shapes are the cross_student_bleed
// eval fixture's job.
func TestExtractNoCrossStudentBleed(t *testing.T) {
	ext := newTestLLMExtractor(t)

	// No collective referent anywhere, so the two halves must not be shared.
	transcript := "Oliver Thursday. Harry was doing great, but today Dinara was not doing that good."

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{Name: "Oliver · Thu", Students: []ClassStudent{{Name: "Dinara"}, {Name: "Harry"}}},
		},
	})
	require.NoError(t, err)

	harry := noteOf(t, result, "Harry")
	assert.True(t, contains(harry, "doing great"), "Harry's note is missing his own observation: %s", harry)
	assert.NotContains(t, harry, "Dinara", "Harry's note leaked Dinara's observation: %s", harry)

	dinara := noteOf(t, result, "Dinara")
	assert.True(t, contains(dinara, "not doing that good"), "Dinara's note is missing her own observation: %s", dinara)
	assert.NotContains(t, dinara, "Harry", "Dinara's note leaked Harry's observation: %s", dinara)
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

	notes, _ := assemblePassages(passages, science.Students)
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

// Absence is a kind, not a phrasing (#142). The eval rows on these two
// fixtures grade notes, and an absent child's note is the same text a child
// passage would have made, so the kind itself is invisible there. This is
// where it is pinned, against the real passageSchema — the enum the eval
// config only keeps a hand-written copy of.
//
// Both fixtures in one test: one model call each, and the pair is the
// measurement — the phrasing the prompt spells out, and phrasings it does not.
func TestLLM_NamesAChildTheTeacherSaidWasAbsent(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		absent  []string
		present string
	}{
		{fixture: "absent_child", absent: []string{"Théo"}, present: "Camille"},
		{fixture: "absent_phrasing", absent: []string{"Théo", "Lina"}, present: "Basile"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			ext := newTestLLMExtractor(t)
			transcript, classes := extractionFixture(t, tc.fixture)

			result, err := ext.Extract(t.Context(), ExtractRequest{Transcript: transcript, Classes: classes})
			require.NoError(t, err)

			kinds := map[string]PassageKind{}
			for _, p := range result.Passages {
				if p.Student != "" {
					kinds[p.Student] = p.Kind
				}
			}
			for _, name := range tc.absent {
				assert.Equal(t, PassageAbsent, kinds[name],
					"%s was named absent; passages: %+v", name, result.Passages)
			}
			assert.Equal(t, PassageChild, kinds[tc.present],
				"%s was there; passages: %+v", tc.present, result.Passages)
		})
	}
}

// --- The decline, and the pick that undoes it (#127) ---

// extractionFixture reads one eval fixture's transcript and roster off disk.
//
// Read rather than inlined, so a live test and the promptfoo row that shares a
// fixture cannot drift apart. multi_class left promptfoo when extraction became
// two calls — the pass-2 builder needs a class_name var and that fixture's
// class is the thing under test — so for it this is the only place it is still
// exercised.
func extractionFixture(t *testing.T, name string) (transcript string, classes []ClassGroup) {
	t.Helper()
	dir := filepath.Join("evals", "fixtures", "extraction", name)
	raw, err := os.ReadFile(filepath.Join(dir, "transcript.txt"))
	require.NoError(t, err)
	rosterJSON, err := os.ReadFile(filepath.Join(dir, "classes.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rosterJSON, &classes))
	return strings.TrimSpace(string(raw)), classes
}

// The decline itself. Pass 1 runs against the real classPickSchema — the enum
// this task added "" to — and the recording names two classes with no header,
// so it must return "" rather than guessing one.
//
// Not a promptfoo row: every promptfoo route re-implements the schema, and this
// task's whole change is one value inside classPickSchema. Testing a replica of
// it would test the wrong thing.
//
// The empty passage list matters as much as the empty class name. A total
// extraction failure also yields no notes, and it must not score as a correct
// decline.
func TestLLM_DeclinesWhenNoHeaderPinsOneClass(t *testing.T) {
	ext := newTestLLMExtractor(t)
	transcript, classes := extractionFixture(t, "multi_class")

	result, err := ext.Extract(t.Context(), ExtractRequest{Transcript: transcript, Classes: classes})
	require.NoError(t, err, "a decline is a finished recording, not a failed one")

	assert.Empty(t, result.ClassName, "two classes named and no header: pass 1 must decline")
	assert.Empty(t, result.Passages, "a decline stops before pass 2")
}

// Pass 1 copies the spoken header verbatim, mistranscription included, so the
// cut lands exactly before the first observation (#155). A decline cuts
// nothing: there is no pass 2 to feed.
func TestLLM_PassOneReturnsTheHeaderToCut(t *testing.T) {
	ext := newLLMExtractor(requireLiveLLM(t))

	t.Run("date_drill", func(t *testing.T) {
		transcript, classes := extractionFixture(t, "date_drill")
		className, header, err := ext.pickClass(t.Context(), transcript, classes)
		require.NoError(t, err)
		assert.Equal(t, "Quentin · Wed · 15.20", className)
		assert.True(t, strings.HasPrefix(CutHeader(transcript, header), "Tobin and Marlo did very well."),
			"header %q did not cut exactly", header)
	})

	t.Run("multi_class declines", func(t *testing.T) {
		transcript, classes := extractionFixture(t, "multi_class")
		className, header, err := ext.pickClass(t.Context(), transcript, classes)
		require.NoError(t, err)
		assert.Empty(t, className)
		assert.Equal(t, transcript, CutHeader(transcript, header), "header %q cut a declined recording", header)
	})
}

// The acceptance this task exists for, through the real route: a declined
// recording the teacher files to a class produces the notes the pipeline
// produces when pass 1 pins that same class.
//
// Not string equality — a live model rewords between runs, and a test that
// fires on drift rather than on regression is a flake. The student set, the
// note count, and the phrases the fixture's own expected.json names.
func TestLLM_PickingTheClassOnADeclinedRecordingMakesThePipelineNotes(t *testing.T) {
	ext := newTestLLMExtractor(t)
	transcript, fixture := extractionFixture(t, "multi_class")
	w := newLiveAssembleWorld(t, transcript, fixture)
	classA := w.classes[0]

	// What the pipeline makes of the same recording when its header pins the
	// class. This is the yardstick: the header is the only difference.
	pinned, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: classA.Name + ". " + transcript,
		Classes:    w.classes,
	})
	require.NoError(t, err)
	require.Equal(t, classA.Name, pinned.ClassName, "the yardstick run must pin the class")
	wantNotes, _ := assemblePassages(pinned.Passages, classA.Students)
	require.NotEmpty(t, wantNotes, "the yardstick made no note; passages: %+v", pinned.Passages)

	// And now the declined recording, filed by hand through the endpoint.
	rec := w.serve(t, t.Context(), "u1", w.uploadID, AssembleNotesRequest{ClassName: classA.Name})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp AssembleNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, classA.Name, resp.ClassName)

	got := make([]string, len(resp.NoteLinks))
	for i, l := range resp.NoteLinks {
		got[i] = l.Name
	}
	want := make([]string, len(wantNotes))
	for i, n := range wantNotes {
		want[i] = n.Name
	}
	assert.ElementsMatch(t, want, got, "the pick must reach the children the pipeline reaches")

	// Each note holds what the recording said about that child. The fixture's
	// must_not_extract list is the phrases belonging to the other class; the
	// two below are Noah's, and they are the whole of what Class A was told.
	require.Len(t, resp.NoteLinks, 1)
	notes, err := w.noteRepo.List(t.Context(), resp.NoteLinks[0].StudentID)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.True(t, contains(notes[0].Summary, "writing") || contains(notes[0].Summary, "sentence"),
		"the note lost what the recording said: %q", notes[0].Summary)
	assert.False(t, contains(notes[0].Summary, "science experiment"),
		"another class's observation reached this note: %q", notes[0].Summary)
}

// liveAssembleWorld is one teacher, the fixture's classes, and a declined
// recording, wired to the live extractor and driven through the real router.
//
// The route is the point. The eval harness would exercise neither it, nor the
// ownership gates, nor the job update — and this task's change is as much where
// pass 2 runs as what it returns.
type liveAssembleWorld struct {
	noteRepo *NoteRepo
	uploadID int64
	// classes is the roster as the database composes it — a class name is level
	// plus time slot, so it is not the fixture's own string. Both the pick and
	// the pipeline run it reads must use these, or they are not comparable.
	classes []ClassGroup
}

func newLiveAssembleWorld(t *testing.T, transcript string, classes []ClassGroup) *liveAssembleWorld {
	t.Helper()
	ctx := t.Context()
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}
	noteRepo := &NoteRepo{db: db}
	voiceNotes := &VoiceNoteRepo{db: db}

	for _, c := range classes {
		cls := newTestClass(t, classRepo, "test-group", "u1", c.Name, "")
		for _, s := range c.Students {
			_, err := studentRepo.Create(ctx, cls.ID, s.Name)
			require.NoError(t, err)
		}
	}

	vn, err := voiceNotes.Create(ctx, "u1", "declined.m4a", "/nowhere/declined.m4a")
	require.NoError(t, err)
	require.NoError(t, voiceNotes.SetTranscript(ctx, vn.ID, transcript))

	queue := newStubVoiceNoteQueue()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: vn.ID, FileName: "declined.m4a",
		Status: JobStatusDone, NoNotesReason: NoNotesClassUnclear,
	}))

	roster := newDBRoster(classRepo, studentRepo, "u1")
	withDeps(t, &mockDepsAll{
		db:             db,
		classRepo:      classRepo,
		studentRepo:    studentRepo,
		noteRepo:       noteRepo,
		voiceNoteRepo:  voiceNotes,
		voiceNoteQueue: queue,
		roster:         roster,
		extractor:      newTestLLMExtractor(t),
		noteCreator:    newDBNoteCreator(noteRepo),
	})

	stored, err := roster.Students(ctx)
	require.NoError(t, err)
	require.Len(t, stored, len(classes))
	return &liveAssembleWorld{noteRepo: noteRepo, uploadID: vn.ID, classes: stored}
}

func (w *liveAssembleWorld) serve(t *testing.T, ctx context.Context, user string, uploadID int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/voice-notes/%d/assemble", uploadID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newAPIMux(fakeAuth(user, "test-group", "org:member")).ServeHTTP(rec, req.WithContext(ctx))
	return rec
}
