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
				Students: []MatchedStudent{
					{Name: "Alice", ClassName: "Math · Mon", QuotedText: "Did great", Confidence: 0.9},
					{Name: "Bob", ClassName: "Math · Mon", QuotedText: "Needs improvement", Confidence: 0.8},
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

	// The done card's wire surface. Every mention becomes a passage whether or
	// not it became a note, and the ones that did carry the child they reached
	// — that is what stops the card offering a class picker over notes that
	// already exist.
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
			Students: []MatchedStudent{{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9}},
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

func TestProcessJob_WrongClassSkipped(t *testing.T) {
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
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9},
				{Name: "Alice", ClassName: "WrongClass", QuotedText: "hallucinated", Confidence: 0.9},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)), "processVoiceNote should succeed despite wrong class")

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)
	assert.Len(t, nc.calls, 1, "note creator calls: wrong class should be skipped")

	// The dropped mention is still a passage, with no child on it: it is
	// exactly the one a class pick can rescue.
	require.Len(t, got.Passages, 2)
	assert.Equal(t, "Alice", got.Passages[0].Student)
	assert.Empty(t, got.Passages[1].Student)
	assert.Equal(t, "hallucinated", got.Passages[1].Summary)
}

func TestProcessJob_LowConfidenceSkipped(t *testing.T) {
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
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9},
				{Name: "Maybe", ClassName: "Math · Mon", QuotedText: "unsure", Confidence: 0.3},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))
	assert.Len(t, nc.calls, 1, "note creator calls: low confidence should be skipped")

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
	require.NoError(t, err)
	require.Len(t, got.Passages, 2)
	assert.Equal(t, "Alice", got.Passages[0].Student)
	assert.Empty(t, got.Passages[1].Student, "a mention dropped below the confidence gate reached nobody")
}

// TestProcessJob_QuotedTextPassedToNoteCreator verifies that QuotedText from
// extraction flows through to CreateNoteRequest without modification.
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
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: rawQuote, Confidence: 0.95},
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
			Students: []MatchedStudent{{Name: "Alice", ClassName: "Math · Mon", QuotedText: "did well", Confidence: 0.9}},
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
// mention dropped, zero notes created, and the transcript is still on the
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
			// One roster miss, one below the confidence gate: both drop paths, no note.
			Students: []MatchedStudent{
				{Name: "Unknown", ClassName: "Math · Mon", QuotedText: "x", Confidence: 0.9},
				{Name: "Unknown", ClassName: "Math · Mon", QuotedText: "y", Confidence: 0.1},
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
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9},
				// Dropped: below the auto-create confidence threshold. The candidates are
				// what candidate_count counts, and their names must not escape either.
				{Name: "Quillon", ClassName: "Math · Mon", QuotedText: "unsure", Confidence: 0.3,
					Candidates: []StudentCandidate{
						{Name: "Quintus", ClassName: "Math · Mon"},
						{Name: "Quiller", ClassName: "Math · Mon"},
					}},
				// Dropped: confidently extracted, but not on the roster.
				{Name: "Zephyrine", ClassName: "Math · Mon", QuotedText: "hallucinated", Confidence: 0.9},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx, logs := captureLogs(context.Background())
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))
	require.Len(t, nc.calls, 1, "note creator calls: both low-confidence and off-roster students should be dropped")

	out := logs.String()
	// Both sites emit one shared message string, so a record is identified by its
	// reason rather than by its message.
	require.Contains(t, out, `"reason":"low_confidence"`, "low-confidence drop was not logged at all")
	require.Contains(t, out, `"reason":"no_roster_match"`, "off-roster drop was not logged at all")

	assert.NotContains(t, out, "Quillon", "low-confidence drop leaked a student name into the logs")
	assert.NotContains(t, out, "Zephyrine", "off-roster drop leaked a student name into the logs")
	// Only the count of candidate matches may escape, never the candidates themselves.
	assert.NotContains(t, out, "Quintus", "candidate_count leaked a candidate's name into the logs")
	assert.NotContains(t, out, "Quiller", "candidate_count leaked a candidate's name into the logs")

	// key is fmt.Sprintf("%s/%d", userID, uploadID) (voice_note_job.go), so it is
	// redundant with the user_id/upload_id fields beside it. It is asserted for
	// field-set uniformity with the completion record, not because it is the only
	// thing tying a drop to a teacher and an upload — it no longer is.
	lowConf := logRecord(t, out, `"reason":"low_confidence"`)
	assert.Contains(t, lowConf, "process voice note: mention dropped", "both drop sites must share the stable query key")
	assert.Contains(t, lowConf, `"key":"u1/1"`, "low-confidence drop should carry the job key")
	assert.Contains(t, lowConf, `"confidence":0.3`, "low-confidence drop should keep the confidence that caused it")
	// By value, not just by key: 0 is the ambiguous answer here, indistinguishable
	// from logging the wrong expression, and candidate_count exists to settle whether
	// a review UI could pre-populate a picker.
	assert.Contains(t, lowConf, `"candidate_count":2`, "low-confidence drop should carry the number of candidate matches")
	// The Change spec names user_id and upload_id explicitly, and class_name is here
	// so both drop records carry an identical field set and aggregate cleanly. Locked
	// by assertion so neither can be dropped as redundant-looking noise.
	assert.Contains(t, lowConf, `"user_id":"u1"`, "low-confidence drop should carry the user id")
	assert.Contains(t, lowConf, `"upload_id":1`, "low-confidence drop should carry the upload id")
	assert.Contains(t, lowConf, `"class_name"`, "both drop records should carry the same field set")
	// Model and prompt version turn a bare drop rate into a figure attributable to a
	// specific model/prompt change (#96).
	assert.Contains(t, lowConf, `"model":"test-model-v1"`, "low-confidence drop should carry the model that produced the extraction")
	assert.Contains(t, lowConf, promptHashAttr, "low-confidence drop should carry the extraction prompt hash")

	offRoster := logRecord(t, out, `"reason":"no_roster_match"`)
	assert.Contains(t, offRoster, "process voice note: mention dropped", "both drop sites must share the stable query key")
	assert.Contains(t, offRoster, `"key":"u1/1"`, "off-roster drop should carry the job key")
	// By value: production names class_name the diagnostic field for this reason —
	// the one observed production drop of this kind was a malformed class name — so
	// an empty one would defeat the readout while still passing a presence check.
	assert.Contains(t, offRoster, `"class_name":"Math · Mon"`, "off-roster drop should keep the class it was attributed to")
	assert.Contains(t, offRoster, `"user_id":"u1"`, "off-roster drop should carry the user id")
	assert.Contains(t, offRoster, `"upload_id":1`, "off-roster drop should carry the upload id")
	assert.Contains(t, offRoster, `"model":"test-model-v1"`, "off-roster drop should carry the model that produced the extraction")
	assert.Contains(t, offRoster, promptHashAttr, "off-roster drop should carry the extraction prompt hash")
}

// TestProcessJob_CompletionRecordCountsMentions covers the denominator half of the
// drop instrumentation: a bare count of drops cannot be read as a rate, so the
// completion record has to say how many mentions extraction produced.
//
// Every expected value is deliberately distinct — 7/2/1/4/3 — because counters that
// all happen to be 1 cannot catch a counter wired to the wrong variable. The
// fixture is sized for that discrimination, not for realism.
func TestProcessJob_CompletionRecordCountsMentions(t *testing.T) {
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
			Students: []MatchedStudent{
				// 2 notes: on the roster and over the gate. Bram is also under the
				// headroom ceiling, so "below 0.7" cannot be read as "was dropped".
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9},
				{Name: "Bram", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.65},
				// 1 low-confidence drop, under the headroom ceiling.
				{Name: "Quillon", ClassName: "Math · Mon", QuotedText: "unsure", Confidence: 0.3},
				// 4 off-roster drops; only Wim is under the headroom ceiling.
				{Name: "Zephyrine", ClassName: "Math · Mon", QuotedText: "hallucinated", Confidence: 0.9},
				{Name: "Xander", ClassName: "Math · Mon", QuotedText: "hallucinated", Confidence: 0.95},
				{Name: "Yara", ClassName: "Math · Mon", QuotedText: "hallucinated", Confidence: 0.85},
				{Name: "Wim", ClassName: "Math · Mon", QuotedText: "hallucinated", Confidence: 0.55},
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
	assert.Contains(t, done, `"mentions_total":7`, "denominator should count every mention extraction returned")
	assert.Contains(t, done, `"note_count":2`, "only roster-matched mentions over the gate become notes")
	assert.Contains(t, done, `"dropped_low_confidence":1`)
	assert.Contains(t, done, `"dropped_no_roster_match":4`)
	// Bram (0.65, kept) + Quillon (0.3, dropped) + Wim (0.55, dropped) = 3. Counting
	// every mention under 0.7 regardless of outcome is the point: this is the total at
	// a stricter gate, not the extra drops moving the gate there would cause.
	assert.Contains(t, done, `"mentions_below_0_7":3`, "headroom counter should span kept and dropped mentions alike")
	// Model and prompt version turn the drop rate into a figure attributable to a
	// specific model/prompt change (#96).
	assert.Contains(t, done, `"model":"test-model-v1"`, "completion record should carry the model that produced the extraction")
	assert.Contains(t, done, promptHashAttr, "completion record should carry the extraction prompt hash")
}

// TestProcessJob_CompletionRecordNamesZeroMentionMode covers the third
// silent-nothing mode: extraction returns no mentions at all, so the job completes
// with no notes and no drops. It is indistinguishable from a job whose mentions
// were all dropped unless mentions_total says so.
func TestProcessJob_CompletionRecordNamesZeroMentionMode(t *testing.T) {
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
	assert.Contains(t, done, `"mentions_total":0`, "zero-mention mode is what mentions_total:0 names")
	assert.Contains(t, done, `"note_count":0`)
	assert.Contains(t, done, `"dropped_low_confidence":0`)
	assert.Contains(t, done, `"dropped_no_roster_match":0`)
	assert.NotContains(t, out, "mention dropped", "no mentions means nothing to drop")
}

// wantExtractionPromptHash recomputes the expected hash from the prompt templates
// via hashPrompt, independently of the ExtractionPromptHash package var — so a
// mutation that blanks that var, or that logs some other package's hash (e.g.
// ReportPromptHash) instead, is caught rather than passing on a same-value or
// right-shape coincidence.
var wantExtractionPromptHash = hashPrompt(extractionPromptPrefix + "<<<roster>>>" + extractionPromptSuffix)

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
				Students: []MatchedStudent{
					{Name: "Zephyrine", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9},
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
// class picker is offered on, so the reason has to say which of the two silent
// outcomes it was.
func TestProcessJob_NoNotesReasonAndClassName(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mentions   []MatchedStudent
		wantReason string
		wantClass  string
		wantCount  int
	}{
		{
			name:       "nobody named",
			mentions:   nil,
			wantReason: NoNotesNobodyNamed,
		},
		{
			name:       "named, but nobody on the roster",
			mentions:   []MatchedStudent{{Name: "Nobody", ClassName: "WrongClass", QuotedText: "said something", Confidence: 0.9}},
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
				extractor:     &stubExtractor{result: &ExtractResponse{Students: tc.mentions}},
				noteCreator:   &stubNoteCreator{},
				studentRepo:   studentRepo,
				voiceNoteRepo: voiceNoteRepo,
			}

			ctx := context.Background()
			require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: uploadID, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
			require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", uploadID)))

			got, err := queue.GetJob(ctx, voiceNoteKey("u1", uploadID))
			require.NoError(t, err)
			assert.Empty(t, got.NoteLinks)
			assert.Equal(t, tc.wantReason, got.NoNotesReason)
			assert.Equal(t, tc.wantClass, got.ClassName)
			assert.Len(t, got.Passages, tc.wantCount)
		})
	}
}

// The card shows one class or none. A recording whose notes went to two classes
// has no single answer, and naming one of them would be a guess about which the
// teacher meant.
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
