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
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)))
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
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)))

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
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)))

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
