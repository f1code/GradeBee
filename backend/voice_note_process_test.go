package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessJob_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	// Seed class + students.
	cls := newTestClass(t, classRepo, "test-group", "user1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Bob")
	require.NoError(t, err)

	// Write a temp audio file.
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "user1", audioPath)
	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{
		results: []*CreateNoteResponse{
			{NoteID: 1},
			{NoteID: 2},
		},
	}
	transcriber := &stubTranscriber{result: "Alice did great today. Bob needs improvement."}
	d := &mockDepsAll{
		transcriber: transcriber,
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}, {Name: "Bob"}}}},
		},
		extractor: &stubExtractor{
			result: &ExtractResponse{
				ClassName: "Math · Mon",
				Passages: []ExtractedPassage{
					{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "Did great"},
					{Kind: PassageChild, SpokenLabels: []string{"Bob"}, Student: "Bob", Summary: "Needs improvement"},
				},
			},
		},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	job := VoiceNoteJob{
		UserID:    "user1",
		UploadID:  uploadID,
		FilePath:  audioPath,
		FileName:  "recording.m4a",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}
	require.NoError(t, queue.Publish(ctx, job))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("user1", uploadID)))

	got, err := queue.GetJob(ctx, voiceNoteKey("user1", uploadID))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)
	assert.Len(t, got.NoteLinks, 2)
	assert.Len(t, nc.calls, 2)
	assert.Equal(t, []string{"Math"}, transcriber.gotBias, "transcriber should receive class names as context bias")

	// The done card's wire surface. Every passage extraction returned is here
	// whether or not it became a note, and the ones that did carry the child
	// they reached — that is what stops the card offering a class picker over
	// notes that already exist.
	require.Len(t, got.Passages, 2)
	assert.Equal(t, []JobPassage{
		{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "Did great"},
		{Kind: PassageChild, SpokenLabels: []string{"Bob"}, Student: "Bob", Summary: "Needs improvement"},
	}, got.Passages)
	assert.Equal(t, "Math · Mon", got.ClassName)
	assert.Empty(t, got.NoNotesReason)
}

func TestProcessJob_TranscribeFail(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{err: io.ErrUnexpectedEOF},
		roster:        &stubRoster{},
		voiceNoteRepo: &VoiceNoteRepo{db: nil}, // won't be called on failure
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	require.Error(t, err)

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	require.NoError(t, err)
	assert.Equal(t, JobStatusFailed, got.Status)
	assert.True(t, strings.Contains(got.Error, "transcribe"), "error = %q, want to contain 'transcribe'", got.Error)
}

func TestProcessJob_ExtractFail(t *testing.T) {
	db := setupTestDB(t)
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	// Create a DB row so MarkPurged has a valid ID to write to.
	vn, err := voiceNoteRepo.Create(t.Context(), "u1", "recording.m4a", audioPath)
	require.NoError(t, err)

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{result: "some transcript"},
		roster:        &stubRoster{},
		extractor:     &stubExtractor{err: io.ErrUnexpectedEOF},
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: vn.ID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))

	err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", vn.ID))
	require.Error(t, err)

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", vn.ID))
	require.NoError(t, err)
	assert.Equal(t, JobStatusFailed, got.Status)

	// The transcript is written before extraction, so a failed extraction does not
	// lose it — the audio is already gone by then.
	row, err := voiceNoteRepo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	require.NotNil(t, row.Transcript, "transcript should be persisted before extraction runs")
	assert.Equal(t, "some transcript", *row.Transcript)
}

func TestProcessJob_NoteCreateFail(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages:  []ExtractedPassage{{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "ok"}},
		}},
		noteCreator:   &stubNoteCreator{err: io.ErrUnexpectedEOF},
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))

	err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	require.Error(t, err)

	got, gErr := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	require.NoError(t, gErr)
	assert.Equal(t, JobStatusFailed, got.Status)
}

func TestProcessJob_AlreadyProcessed(t *testing.T) {
	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{}

	ctx := context.Background()
	queue.jobs[voiceNoteKey("u1", 1)] = VoiceNoteJob{
		UserID: "u1", UploadID: 1, Status: JobStatusDone,
	}

	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)), "expected no error for already-processed job")

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status, "status changed, should remain done")
}

// TestProcessJob_StudentMissingFromRosterSkipped: pass 2's schema constrains
// student to the pinned class's roster, so a name that the lookup cannot find
// means the roster read and the lookup disagreed — a child deleted mid-run. It
// costs that child their note and nobody else theirs.
func TestProcessJob_StudentMissingFromRosterSkipped(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages: []ExtractedPassage{
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "ok"},
				{Kind: PassageChild, SpokenLabels: []string{"Ghost"}, Student: "Ghost", Summary: "vanished"},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)), "processVoiceNote should succeed despite a student the lookup cannot find")

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)
	assert.Len(t, nc.calls, 1, "note creator calls: the missing student should be skipped")

	// The passage stays as extraction wrote it, name and all. It says what the
	// model read the recording as, not what the note store managed to do with
	// it — and its spoken label is what a class pick re-resolves.
	require.Len(t, got.Passages, 2)
	assert.Equal(t, "Alice", got.Passages[0].Student)
	assert.Equal(t, "Ghost", got.Passages[1].Student)
	assert.Equal(t, "vanished", got.Passages[1].Summary)
}

// TestProcessJob_UnattributedPassagesReachNobody: the two ways a passage can
// be about a child and reach none of them. There is no confidence score to
// gate on any more — either the recording named the child or it did not.
func TestProcessJob_UnattributedPassagesReachNobody(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Maybe")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages: []ExtractedPassage{
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "ok"},
				// A name was spoken and nobody on the roster fits it.
				{Kind: PassageChild, SpokenLabels: []string{"Polly"}, Student: "", Summary: "unsure"},
				// No name was spoken at all.
				{Kind: PassageUnknown, Student: "", Summary: "she got on with it"},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))
	assert.Len(t, nc.calls, 1, "note creator calls: a passage naming nobody should reach nobody")

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
	require.NoError(t, err)
	require.Len(t, got.Passages, 3)
	assert.Equal(t, "Alice", got.Passages[0].Student)
	assert.Empty(t, got.Passages[1].Student, "a spoken name matching nobody reached nobody")
	// The label survives on the unmatched name and not on the unknown: one has
	// something for a class pick to re-resolve, the other never said a name.
	assert.Equal(t, []string{"Polly"}, got.Passages[1].SpokenLabels)
	assert.Empty(t, got.Passages[2].Student)
	assert.Empty(t, got.Passages[2].SpokenLabels)
}

// TestProcessJob_QuotedTextPassedToNoteCreator verifies that a passage's
// summary flows through to CreateNoteRequest without modification. The summary
// is the note's visible text, so anything rewriting it here rewrites what the
// teacher reads under the model's name.
func TestProcessJob_QuotedTextPassedToNoteCreator(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}

	rawQuote := "Alice was impossibly good today - she blew my mind with her presentation"

	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "some transcript"},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}}}},
		},
		extractor: &stubExtractor{result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages: []ExtractedPassage{
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: rawQuote},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))

	require.Len(t, nc.calls, 1, "expected 1 note creation call")
	assert.Equal(t, rawQuote, nc.calls[0].QuotedText, "QuotedText not passed through")
}

// TestProcessJob_DeletesAudioAfterTranscription verifies that the audio file is
// deleted from disk and purged_at is set in the DB immediately after transcription.
func TestProcessJob_DeletesAudioAfterTranscription(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	// Create a voice note row so MarkPurged has a real ID to update.
	vn, err := voiceNoteRepo.Create(t.Context(), "u1", "recording.m4a", audioPath)
	require.NoError(t, err)

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "Alice did well"},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}}}},
		},
		extractor: &stubExtractor{result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages:  []ExtractedPassage{{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "did well"}},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: vn.ID, FilePath: audioPath,
		FileName: "recording.m4a", Status: JobStatusQueued, CreatedAt: time.Now(),
	}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", vn.ID)))

	// Audio file must be gone.
	_, statErr := os.Stat(audioPath)
	assert.True(t, os.IsNotExist(statErr), "expected audio file to be deleted after transcription")

	// purged_at must be set in DB.
	got, err := voiceNoteRepo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.PurgedAt, "expected PurgedAt to be set after transcription")

	// purged_at marks the audio file alone. The transcript stays on the row — it is
	// the only copy once the audio is gone — and leaves with the row when the
	// retention cleanup deletes it (see voice_note_cleanup_test.go).
	require.NotNil(t, got.Transcript, "transcript should survive the audio purge")
	assert.Equal(t, "Alice did well", *got.Transcript)
}

// TestProcessJob_PersistsTranscriptWithoutNotes covers the case #80 depends on: every
// passage reached nobody, zero notes created, and the transcript is still on the
// voice_notes row. Before this, notes.transcript was the only copy, so a job that
// created no note left the teacher's words nowhere.
func TestProcessJob_PersistsTranscriptWithoutNotes(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	vn, err := voiceNoteRepo.Create(t.Context(), "u1", "recording.m4a", audioPath)
	require.NoError(t, err)

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "Nobody on the roster did anything"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			ClassName: "Math · Mon",
			// A spoken name nobody answers to, and a block with no name at all:
			// both ways to reach nobody, no note.
			Passages: []ExtractedPassage{
				{Kind: PassageChild, SpokenLabels: []string{"Polly"}, Student: "", Summary: "x"},
				{Kind: PassageUnknown, Student: "", Summary: "y"},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: vn.ID, FilePath: audioPath,
		FileName: "recording.m4a", Status: JobStatusQueued, CreatedAt: time.Now(),
	}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", vn.ID)))

	assert.Empty(t, nc.calls, "no note should be created")

	got, err := voiceNoteRepo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Transcript, "transcript should be persisted even when no note is created")
	assert.Equal(t, "Nobody on the roster did anything", *got.Transcript)
}

// TestProcessJob_PersistFailureFailsJobThenRetries: a failed transcript write fails
// the job, so the teacher can retry, but the audio is deleted regardless — the
// privacy page promises the recording is gone right after transcription. The
// failed job keeps the transcript, so the retry skips transcription and only
// repeats the write.
func TestProcessJob_PersistFailureFailsJobThenRetries(t *testing.T) {
	db := setupTestDB(t)
	goodRepo := &VoiceNoteRepo{db: db}
	audioPath := newTestAudio(t)
	uploadID := newTestVoiceNote(t, goodRepo, "u1", audioPath)

	closed, err := OpenDB(":memory:")
	require.NoError(t, err)
	require.NoError(t, closed.Close())

	queue := newStubVoiceNoteQueue()
	transcriber := &stubTranscriber{result: "words"}
	d := &mockDepsAll{
		transcriber:   transcriber,
		roster:        &stubRoster{},
		extractor:     &stubExtractor{result: &ExtractResponse{}},
		noteCreator:   &stubNoteCreator{},
		voiceNoteRepo: &VoiceNoteRepo{db: closed},
	}

	ctx := context.Background()
	key := voiceNoteKey("u1", uploadID)
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: uploadID, FilePath: audioPath,
		FileName: "recording.m4a", Status: JobStatusQueued, CreatedAt: time.Now(),
	}))
	require.Error(t, processVoiceNote(ctx, d, queue, key))

	_, statErr := os.Stat(audioPath)
	assert.True(t, os.IsNotExist(statErr), "audio must be deleted even when the transcript could not be persisted")

	got, err := queue.GetJob(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, JobStatusFailed, got.Status)
	assert.Contains(t, got.Error, "persist transcript")
	assert.Equal(t, "words", got.Transcript, "failed job keeps the transcript for retry")

	// Retry with the store back: no audio, no transcription, just the write.
	d.voiceNoteRepo = goodRepo
	transcriber.err = io.ErrUnexpectedEOF
	got.Status = JobStatusQueued
	require.NoError(t, queue.UpdateJob(ctx, *got))
	require.NoError(t, processVoiceNote(ctx, d, queue, key))

	got, err = queue.GetJob(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)
	row, err := goodRepo.GetByID(ctx, uploadID)
	require.NoError(t, err)
	require.NotNil(t, row.Transcript)
	assert.Equal(t, "words", *row.Transcript)
}

// TestProcessJob_RetryPastRetention: once the cleanup has swept an upload, a retry
// finds either no audio file (job failed before transcription) or no voice_notes
// row (job failed after). Both tell the teacher the upload is too old, with no
// raw error appended, while the telemetry step stays the raw one so saved Sentry
// queries keep matching.
func TestProcessJob_RetryPastRetention(t *testing.T) {
	const tooOld = "this upload is too old to retry"

	t.Run("audio file gone", func(t *testing.T) {
		db := setupTestDB(t)
		voiceNoteRepo := &VoiceNoteRepo{db: db}
		audioPath := filepath.Join(t.TempDir(), "swept.m4a") // never written
		uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
		d := &mockDepsAll{
			transcriber:   &stubTranscriber{result: "unreachable: the open fails first"},
			roster:        &stubRoster{},
			voiceNoteRepo: voiceNoteRepo,
		}

		queue := newStubVoiceNoteQueue()
		ctx, logs := captureLogs(context.Background())
		require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
		require.Error(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))

		assert.Contains(t, logs.String(), `"step":"open audio file"`)
		got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
		require.NoError(t, err)
		assert.Equal(t, JobStatusFailed, got.Status)
		assert.Equal(t, tooOld, got.Error)
	})

	t.Run("row gone", func(t *testing.T) {
		db := setupTestDB(t)
		voiceNoteRepo := &VoiceNoteRepo{db: db}
		uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", "")
		require.NoError(t, voiceNoteRepo.Delete(t.Context(), uploadID))
		d := &mockDepsAll{
			transcriber:   &stubTranscriber{err: io.ErrUnexpectedEOF}, // skipped: the job carries its transcript
			roster:        &stubRoster{},
			voiceNoteRepo: voiceNoteRepo,
		}

		queue := newStubVoiceNoteQueue()
		ctx, logs := captureLogs(context.Background())
		require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, Transcript: "kept on the job", Status: JobStatusQueued, CreatedAt: time.Now()}))
		require.Error(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))

		assert.Contains(t, logs.String(), `"step":"persist transcript"`)
		got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
		require.NoError(t, err)
		assert.Equal(t, JobStatusFailed, got.Status)
		assert.Equal(t, tooOld, got.Error)
	})
}

// TestProcessJob_PersistsPastedText: a text job never transcribes, and its
// voice_notes row has no file. The pasted text is still written to the row so both
// input kinds leave the same trail.
func TestProcessJob_PersistsPastedText(t *testing.T) {
	db := setupTestDB(t)
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	vn, err := voiceNoteRepo.Create(t.Context(), "u1", "pasted-text", "")
	require.NoError(t, err)

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{err: io.ErrUnexpectedEOF}, // must not be called
		roster:        &stubRoster{},
		extractor:     &stubExtractor{result: &ExtractResponse{}},
		noteCreator:   &stubNoteCreator{},
		studentRepo:   &StudentRepo{db: db},
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: vn.ID, FileName: "pasted-text", Source: "text",
		Transcript: "Typed by the teacher", Status: JobStatusQueued, CreatedAt: time.Now(),
	}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", vn.ID)))

	got, err := voiceNoteRepo.GetByID(ctx, vn.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Transcript)
	assert.Equal(t, "Typed by the teacher", *got.Transcript)
	assert.Nil(t, got.PurgedAt, "a text job has no audio to purge")
}

// TestProcessJob_DropSitesOmitStudentName locks in ADR 0003: neither silent-drop
// path may put a student name in the logs, because the log handler ships them to
// Sentry. Asserting the records are still emitted keeps the test from passing
// just because the paths never ran, and asserting on the name *value* rather
// than on a field name also catches a name interpolated into a message.
func TestProcessJob_DropSitesOmitStudentName(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Quillon")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{model: "test-model-v1", result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages: []ExtractedPassage{
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "ok"},
				// Dropped: a name was spoken and nobody on the roster fits it. The
				// spoken label is the teacher's word for a child and must not escape.
				{Kind: PassageChild, SpokenLabels: []string{"Quillon"}, Student: "", Summary: "unsure"},
				// Dropped: named a child the lookup cannot find.
				{Kind: PassageChild, SpokenLabels: []string{"Zephyrine"}, Student: "Zephyrine", Summary: "vanished"},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx, logs := captureLogs(context.Background())
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))
	require.Len(t, nc.calls, 1, "note creator calls: both the unattributed and the off-roster passage should be dropped")

	out := logs.String()
	// Both sites emit one shared message string, so a record is identified by its
	// reason rather than by its message.
	require.Contains(t, out, `"reason":"unattributed"`, "unattributed drop was not logged at all")
	require.Contains(t, out, `"reason":"no_roster_match"`, "off-roster drop was not logged at all")

	assert.NotContains(t, out, "Quillon", "unattributed drop leaked a spoken label into the logs")
	assert.NotContains(t, out, "Zephyrine", "off-roster drop leaked a student name into the logs")

	// key is fmt.Sprintf("%s/%d", userID, uploadID) (voice_note_job.go), so it is
	// redundant with the user_id/upload_id fields beside it. It is asserted for
	// field-set uniformity with the completion record, not because it is the only
	// thing tying a drop to a teacher and an upload — it no longer is.
	unattributed := logRecord(t, out, `"reason":"unattributed"`)
	assert.Contains(t, unattributed, "process voice note: mention dropped", "both drop sites must share the stable query key")
	assert.Contains(t, unattributed, `"key":"u1/1"`, "unattributed drop should carry the job key")
	// By value, not just by key: kind separates a passage that spoke a name
	// nobody answers to from one that spoke no name at all, which are different
	// problems with different fixes.
	assert.Contains(t, unattributed, `"kind":"child"`, "unattributed drop should say which kind of passage reached nobody")
	// 0 is the ambiguous answer here, indistinguishable from logging the wrong
	// expression, so assert the count by value.
	assert.Contains(t, unattributed, `"label_count":1`, "unattributed drop should carry how many labels were spoken")
	// The Change spec names user_id and upload_id explicitly, and class_name is here
	// so both drop records carry an identical field set and aggregate cleanly. Locked
	// by assertion so neither can be dropped as redundant-looking noise.
	assert.Contains(t, unattributed, `"user_id":"u1"`, "unattributed drop should carry the user id")
	assert.Contains(t, unattributed, `"upload_id":1`, "unattributed drop should carry the upload id")
	assert.Contains(t, unattributed, `"class_name"`, "both drop records should carry the same field set")
	// Model and prompt version turn a bare drop rate into a figure attributable to a
	// specific model/prompt change (#96).
	assert.Contains(t, unattributed, `"model":"test-model-v1"`, "unattributed drop should carry the model that produced the extraction")
	assert.Contains(t, unattributed, promptHashAttr, "unattributed drop should carry the extraction prompt hash")

	offRoster := logRecord(t, out, `"reason":"no_roster_match"`)
	assert.Contains(t, offRoster, "process voice note: mention dropped", "both drop sites must share the stable query key")
	assert.Contains(t, offRoster, `"key":"u1/1"`, "off-roster drop should carry the job key")
	// By value: class_name is the diagnostic field for this reason, and it is
	// now the class pass 1 pinned for the whole recording — an empty one would
	// defeat the readout while still passing a presence check.
	assert.Contains(t, offRoster, `"class_name":"Math · Mon"`, "off-roster drop should keep the class the recording was pinned to")
	assert.Contains(t, offRoster, `"user_id":"u1"`, "off-roster drop should carry the user id")
	assert.Contains(t, offRoster, `"upload_id":1`, "off-roster drop should carry the upload id")
	assert.Contains(t, offRoster, `"model":"test-model-v1"`, "off-roster drop should carry the model that produced the extraction")
	assert.Contains(t, offRoster, promptHashAttr, "off-roster drop should carry the extraction prompt hash")
}

// TestProcessJob_CompletionRecordCountsPassages covers the denominator half of the
// drop instrumentation: a bare count of drops cannot be read as a rate, so the
// completion record has to say how many passages extraction produced, and of
// which kinds.
//
// Every expected value is deliberately distinct — 9/2/5/2/1/1/3/1 — because
// counters that all happen to be 1 cannot catch a counter wired to the wrong
// variable. The fixture is sized for that discrimination, not for realism.
func TestProcessJob_CompletionRecordCountsPassages(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Bram")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}, {NoteID: 2}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{model: "test-model-v1", result: &ExtractResponse{
			ClassName: "Math · Mon",
			Passages: []ExtractedPassage{
				// 5 child passages. Alice's three fold into one note, so
				// passages_child cannot be read as a note count.
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "ok"},
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "still ok"},
				{Kind: PassageChild, SpokenLabels: []string{"Alice"}, Student: "Alice", Summary: "ok again"},
				{Kind: PassageChild, SpokenLabels: []string{"Bram"}, Student: "Bram", Summary: "ok"},
				// Reached nobody: a spoken name matching no child on the roster.
				{Kind: PassageChild, SpokenLabels: []string{"Quillon"}, Student: "", Summary: "unsure"},
				// 2 more that reached nobody, with no name spoken at all.
				{Kind: PassageUnknown, Summary: "she got on with it"},
				{Kind: PassageUnknown, Summary: "and then she stopped"},
				// 1 group passage, which joins both notes rather than dropping.
				{Kind: PassageGroup, Summary: "everyone worked hard"},
				// 1 header, dropped before it reaches the card.
				{Kind: PassageNone, Summary: "Math, Monday, four fifteen"},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx, logs := captureLogs(context.Background())
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))

	done := logRecord(t, logs.String(), "process voice note completed")
	assert.Contains(t, done, `"passages_total":9`, "denominator should count every passage extraction returned, header included")
	assert.Contains(t, done, `"note_count":2`, "one note per child, however many passages reached them")
	assert.Contains(t, done, `"passages_child":5`)
	assert.Contains(t, done, `"passages_unknown":2`)
	assert.Contains(t, done, `"passages_group":1`)
	assert.Contains(t, done, `"passages_none":1`)
	// Quillon's passage plus the two unknowns. A group passage has no student
	// because it belongs to every child, so it is not a drop.
	assert.Contains(t, done, `"dropped_unattributed":3`, "a passage about one child that reached none of them is the drop")
	// Both children are on the roster, so nothing fails the lookup: 0 has to be
	// distinguishable from the counter never being wired.
	assert.Contains(t, done, `"dropped_no_roster_match":0`)
	// Model and prompt version turn the drop rate into a figure attributable to a
	// specific model/prompt change (#96).
	assert.Contains(t, done, `"model":"test-model-v1"`, "completion record should carry the model that produced the extraction")
	assert.Contains(t, done, promptHashAttr, "completion record should carry the extraction prompt hash")
}

// TestProcessJob_CompletionRecordNamesZeroPassageMode covers the third
// silent-nothing mode: extraction cut the recording into nothing at all, so the
// job completes with no notes and no drops. It is indistinguishable from a job
// whose passages all reached nobody unless passages_total says so.
func TestProcessJob_CompletionRecordNamesZeroPassageMode(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	newTestClass(t, classRepo, "test-group", "u1", "Math", "")

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{result: "transcript"},
		roster:        &stubRoster{},
		extractor:     &stubExtractor{result: &ExtractResponse{}},
		noteCreator:   &stubNoteCreator{},
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx, logs := captureLogs(context.Background())
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))

	out := logs.String()
	done := logRecord(t, out, "process voice note completed")
	assert.Contains(t, done, `"passages_total":0`, "zero-passage mode is what passages_total:0 names")
	assert.Contains(t, done, `"note_count":0`)
	assert.Contains(t, done, `"dropped_unattributed":0`)
	assert.Contains(t, done, `"dropped_no_roster_match":0`)
	assert.NotContains(t, out, "mention dropped", "no passages means nothing to drop")
}

// wantExtractionPromptHash recomputes the expected hash from the prompt templates
// via hashPrompt, independently of the ExtractionPromptHash package var — so a
// mutation that blanks that var, or that logs some other package's hash (e.g.
// ReportPromptHash) instead, is caught rather than passing on a same-value or
// right-shape coincidence.
var wantExtractionPromptHash = hashPrompt(
	classPickPrompt + "<<<classes>>>" + classPickPromptSuffix +
		"<<<pass2>>>" + passagePromptPrefix + "<<<roster>>>")

// promptHashAttr is the exact expected prompt_hash attribute as it appears in a
// log line.
var promptHashAttr = fmt.Sprintf(`"prompt_hash":%q`, wantExtractionPromptHash)

// logRecord returns the single captured record whose message contains substr,
// so assertions apply to one record rather than to the whole log.
func logRecord(t *testing.T, out, substr string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, substr) {
			found = append(found, line)
		}
	}
	require.Len(t, found, 1, "expected exactly one log record containing %q", substr)
	return found[0]
}

// TestProcessJob_FailurePathsOmitStudentName covers the other half of ADR 0003:
// the two fail() paths log at Error, and job_queue_mem re-logs the returned error
// at Error too, so each one becomes a Sentry Issue rather than a log record. These
// are the sites where the name used to be interpolated into the step string.
func TestProcessJob_FailurePathsOmitStudentName(t *testing.T) {
	newDeps := func(studentRepo *StudentRepo, voiceNoteRepo *VoiceNoteRepo, nc NoteCreator) *mockDepsAll {
		return &mockDepsAll{
			transcriber: &stubTranscriber{result: "transcript"},
			roster:      &stubRoster{},
			extractor: &stubExtractor{result: &ExtractResponse{
				ClassName: "Math · Mon",
				Passages: []ExtractedPassage{
					{Kind: PassageChild, SpokenLabels: []string{"Zephyrine"}, Student: "Zephyrine", Summary: "ok"},
				},
			}},
			noteCreator:   nc,
			studentRepo:   studentRepo,
			voiceNoteRepo: voiceNoteRepo,
		}
	}

	t.Run("student lookup fails", func(t *testing.T) {
		db := setupTestDB(t)
		classRepo := &ClassRepo{db: db}
		cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
		_, err := (&StudentRepo{db: db}).Create(t.Context(), cls.ID, "Zephyrine")
		require.NoError(t, err)

		// The voice note lives in its own DB so the transcript write still succeeds
		// once the students DB is closed below.
		voiceNoteRepo := &VoiceNoteRepo{db: setupTestDB(t)}
		audioPath := newTestAudio(t)
		uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)

		d := newDeps(&StudentRepo{db: db}, voiceNoteRepo, &stubNoteCreator{})
		// Close the DB so the lookup fails with something other than ErrNotFound,
		// which is the branch that used to interpolate the name into the step.
		require.NoError(t, db.Close())

		queue := newStubVoiceNoteQueue()
		ctx, logs := captureLogs(context.Background())
		require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))

		err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID))
		require.Error(t, err, "a failed student lookup should fail the job")

		out := logs.String()
		require.Contains(t, out, "process voice note failed", "the failure was not logged at all")
		// Every fail() in the pipeline shares that message, so pin the step: otherwise a
		// fail() promoted earlier would keep this green while no longer covering this branch.
		require.Contains(t, out, `"step":"find student"`, "a different fail() ran; this branch is no longer covered")
		assert.NotContains(t, out, "Zephyrine", "failure step leaked a student name into the logs")
		assert.NotContains(t, err.Error(), "Zephyrine", "returned error leaks the name, and job_queue_mem re-logs it")

		// job.Error is the teacher's copy, not telemetry — it should still name the
		// student, which is the whole point of splitting it from the logged step.
		got, gerr := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
		require.NoError(t, gerr)
		assert.Contains(t, got.Error, "Zephyrine", "the teacher should still be told which student failed")
	})

	t.Run("note creation fails", func(t *testing.T) {
		db := setupTestDB(t)
		classRepo := &ClassRepo{db: db}
		studentRepo := &StudentRepo{db: db}
		cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
		_, err := studentRepo.Create(t.Context(), cls.ID, "Zephyrine")
		require.NoError(t, err)

		nc := &stubNoteCreator{err: errors.New("note store unavailable")}
		voiceNoteRepo := &VoiceNoteRepo{db: db}
		audioPath := newTestAudio(t)
		uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)
		d := newDeps(studentRepo, voiceNoteRepo, nc)

		queue := newStubVoiceNoteQueue()
		ctx, logs := captureLogs(context.Background())
		require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))

		err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID))
		require.Error(t, err, "a failed note creation should fail the job")
		require.Len(t, nc.calls, 1, "note creation should have been attempted")

		out := logs.String()
		require.Contains(t, out, "process voice note failed", "the failure was not logged at all")
		require.Contains(t, out, `"step":"create note for student`, "a different fail() ran; this branch is no longer covered")
		assert.NotContains(t, out, "Zephyrine", "failure step leaked a student name into the logs")
		assert.NotContains(t, err.Error(), "Zephyrine", "returned error leaks the name, and job_queue_mem re-logs it")

		got, gerr := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
		require.NoError(t, gerr)
		assert.Contains(t, got.Error, "Zephyrine", "the teacher should still be told which student failed")
		assert.NotContains(t, got.Error, "student 1", "the teacher should not be shown a raw student id")
	})
}

// newTestVoiceNote inserts the voice_notes row a job needs. processVoiceNote writes
// the transcript to that row and fails the job when it is missing.
func newTestVoiceNote(t *testing.T, repo *VoiceNoteRepo, userID, filePath string) int64 {
	t.Helper()
	vn, err := repo.Create(t.Context(), userID, filepath.Base(filePath), filePath)
	require.NoError(t, err)
	return vn.ID
}

// newTestAudio writes a throwaway audio file and returns its path.
func newTestAudio(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.m4a")
	require.NoError(t, os.WriteFile(path, []byte("audio"), 0o644))
	return path
}

// A recording that named children and gave none of them a note is what the
// class picker is offered on, so the reason has to say which of the three
// silent outcomes it was.
func TestProcessJob_NoNotesReasonAndClassName(t *testing.T) {
	for _, tc := range []struct {
		name string
		// pass1Class is what extraction pinned. Empty is the decline.
		pass1Class string
		passages   []ExtractedPassage
		wantReason string
		wantClass  string
		wantCount  int
	}{
		{
			name:       "nobody named",
			pass1Class: "Math · Mon",
			passages:   nil,
			wantReason: NoNotesNobodyNamed,
		},
		{
			// The decline (#127). Pass 1 could not pin a class — a missing
			// header, or one naming two — so pass 2 never ran and there are no
			// passages at all.
			//
			// This is why the decline cannot go through noNotesReason: with no
			// passages it would answer nobody_named, and the card would hide
			// the class picker on exactly the recording that needs it.
			name:       "the class was not pinned",
			pass1Class: "",
			passages:   nil,
			wantReason: NoNotesClassUnclear,
		},
		{
			// A pinned class whose passages all reached nobody is NOT a
			// decline, even though it also ends with no class on the card:
			// job.ClassName comes from the note links. The teacher's route here
			// is an alias, not a class, and reading the job field instead of
			// pass 1's answer would send them to the wrong one.
			name:       "a class was pinned but nobody was named",
			pass1Class: "Math · Mon",
			passages:   []ExtractedPassage{{Kind: PassageUnknown, Summary: "someone did well"}},
			wantReason: NoNotesNobodyNamed,
			wantCount:  1,
		},
		{
			// A header and nothing else. The none passage is dropped before the
			// card sees it, so this reads as nobody named rather than offering
			// a class picker over a passage there is nothing to pick for.
			name:       "the recording is only a header",
			pass1Class: "Math · Mon",
			passages:   []ExtractedPassage{{Kind: PassageNone, Summary: "Math, Monday, four fifteen"}},
			wantReason: NoNotesNobodyNamed,
		},
		{
			name:       "named, but nobody on the roster",
			pass1Class: "Math · Mon",
			passages:   []ExtractedPassage{{Kind: PassageChild, SpokenLabels: []string{"Polly"}, Summary: "said something"}},
			wantReason: NoNotesNoNameMatched,
			wantCount:  1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			studentRepo := &StudentRepo{db: db}
			classRepo := &ClassRepo{db: db}
			voiceNoteRepo := &VoiceNoteRepo{db: db}

			cls := newTestClass(t, classRepo, "test-group", "u1", "Math", "")
			_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
			require.NoError(t, err)

			audioPath := filepath.Join(t.TempDir(), "test.m4a")
			require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))
			uploadID := newTestVoiceNote(t, voiceNoteRepo, "u1", audioPath)

			queue := newStubVoiceNoteQueue()
			d := &mockDepsAll{
				transcriber:   &stubTranscriber{result: "transcript"},
				roster:        &stubRoster{},
				extractor:     &stubExtractor{result: &ExtractResponse{ClassName: tc.pass1Class, Passages: tc.passages}},
				noteCreator:   &stubNoteCreator{},
				studentRepo:   studentRepo,
				voiceNoteRepo: voiceNoteRepo,
			}

			ctx, buf := captureLogs(context.Background())
			require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
			require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))
			logs := buf.String()

			got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
			require.NoError(t, err)
			assert.Empty(t, got.NoteLinks)
			assert.Equal(t, tc.wantReason, got.NoNotesReason)
			assert.Equal(t, tc.wantClass, got.ClassName)
			assert.Len(t, got.Passages, tc.wantCount)
			// The card's gate travels with the reason, decided here. A
			// recording that named nobody cannot be rescued by a class.
			assert.Equal(t, tc.wantReason != NoNotesNobodyNamed, got.CanPickClass)

			// The completion record has to name the decline, or it is
			// indistinguishable in Sentry from an extraction that returned
			// nothing at all — same passages_total, same zero per-kind counts.
			assert.Contains(t, logs, `"no_notes_reason":"`+tc.wantReason+`"`)
		})
	}
}

// The card shows one class or none. Pass 1 pins one class for the whole
// recording, so the two-class rows are no longer reachable from the pipeline —
// they stay because "" is the answer the class picker needs, and a helper that
// quietly started naming the first class would take it away.
func TestSingleClass(t *testing.T) {
	link := func(class string) NoteLink { return NoteLink{Name: "x", ClassName: class} }
	for _, tc := range []struct {
		name  string
		links []NoteLink
		want  string
	}{
		{"no notes", nil, ""},
		{"one note", []NoteLink{link("Math")}, "Math"},
		{"two notes, one class", []NoteLink{link("Math"), link("Math")}, "Math"},
		{"two classes", []NoteLink{link("Math"), link("French")}, ""},
		{"three, the last one differs", []NoteLink{link("Math"), link("Math"), link("French")}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, singleClass(tc.links))
		})
	}
}
