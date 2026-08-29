package handler

import (
	"context"
	"errors"
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
				Date: "2026-03-22",
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
		UploadID:  1,
		FilePath:  audioPath,
		FileName:  "recording.m4a",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}
	require.NoError(t, queue.Publish(ctx, job))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("user1", 1)))

	got, err := queue.GetJob(ctx, voiceNoteKey("user1", 1))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)
	assert.Len(t, got.NoteLinks, 2)
	assert.Len(t, nc.calls, 2)
	assert.Equal(t, []string{"Math"}, transcriber.gotBias, "transcriber should receive class names as context bias")
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
			Date:     "2026-01-01",
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

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-01-01",
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
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)), "processVoiceNote should succeed despite wrong class")

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	require.NoError(t, err)
	assert.Equal(t, JobStatusDone, got.Status)
	assert.Len(t, nc.calls, 1, "note creator calls: wrong class should be skipped")
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

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-01-01",
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
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)))
	assert.Len(t, nc.calls, 1, "note creator calls: low confidence should be skipped")
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
			Date: "2026-04-13",
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: rawQuote, Confidence: 0.95},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)))

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
			Date:     "2026-04-13",
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

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-01-01",
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9},
				// Dropped: below the auto-create confidence threshold.
				{Name: "Quillon", ClassName: "Math · Mon", QuotedText: "unsure", Confidence: 0.3},
				// Dropped: confidently extracted, but not on the roster.
				{Name: "Zephyrine", ClassName: "Math · Mon", QuotedText: "hallucinated", Confidence: 0.9},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx, logs := captureLogs(context.Background())
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)))
	require.Len(t, nc.calls, 1, "note creator calls: both low-confidence and off-roster students should be dropped")

	out := logs.String()
	require.Contains(t, out, "skipping low-confidence match", "low-confidence drop was not logged at all")
	require.Contains(t, out, "student not found in DB", "off-roster drop was not logged at all")

	assert.NotContains(t, out, "Quillon", "low-confidence drop leaked a student name into the logs")
	assert.NotContains(t, out, "Zephyrine", "off-roster drop leaked a student name into the logs")

	// Queue workers run on a bare context, so the job key is the only thing tying
	// a drop record to a teacher and an upload. Without it the drop is untraceable.
	lowConf := logRecord(t, out, "skipping low-confidence match")
	assert.Contains(t, lowConf, `"key":"u1/1"`, "low-confidence drop should carry the job key")
	assert.Contains(t, lowConf, `"confidence"`, "low-confidence drop should keep the confidence that caused it")

	offRoster := logRecord(t, out, "student not found in DB")
	assert.Contains(t, offRoster, `"key":"u1/1"`, "off-roster drop should carry the job key")
	assert.Contains(t, offRoster, `"class_name"`, "off-roster drop should keep the class it was attributed to")
}

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
				Date: "2026-01-01",
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

		d := newDeps(&StudentRepo{db: db}, &VoiceNoteRepo{db: db}, &stubNoteCreator{})
		// Close the DB so the lookup fails with something other than ErrNotFound,
		// which is the branch that used to interpolate the name into the step.
		require.NoError(t, db.Close())

		queue := newStubVoiceNoteQueue()
		ctx, logs := captureLogs(context.Background())
		require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: newTestAudio(t), Status: JobStatusQueued, CreatedAt: time.Now()}))

		err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
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
		got, gerr := queue.GetJob(ctx, voiceNoteKey("u1", 1))
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
		d := newDeps(studentRepo, &VoiceNoteRepo{db: db}, nc)

		queue := newStubVoiceNoteQueue()
		ctx, logs := captureLogs(context.Background())
		require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: newTestAudio(t), Status: JobStatusQueued, CreatedAt: time.Now()}))

		err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
		require.Error(t, err, "a failed note creation should fail the job")
		require.Len(t, nc.calls, 1, "note creation should have been attempted")

		out := logs.String()
		require.Contains(t, out, "process voice note failed", "the failure was not logged at all")
		require.Contains(t, out, `"step":"create note for student`, "a different fail() ran; this branch is no longer covered")
		assert.NotContains(t, out, "Zephyrine", "failure step leaked a student name into the logs")
		assert.NotContains(t, err.Error(), "Zephyrine", "returned error leaks the name, and job_queue_mem re-logs it")

		got, gerr := queue.GetJob(ctx, voiceNoteKey("u1", 1))
		require.NoError(t, gerr)
		assert.Contains(t, got.Error, "Zephyrine", "the teacher should still be told which student failed")
		assert.NotContains(t, got.Error, "student 1", "the teacher should not be shown a raw student id")
	})
}

// newTestAudio writes a throwaway audio file and returns its path.
func newTestAudio(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.m4a")
	require.NoError(t, os.WriteFile(path, []byte("audio"), 0o644))
	return path
}
