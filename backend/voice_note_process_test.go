package handler

import (
	"context"
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
	cls, err := classRepo.Create(t.Context(), "user1", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
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
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "Alice did great today. Bob needs improvement."},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}, {Name: "Bob"}}}},
		},
		extractor: &stubExtractor{
			result: &ExtractResponse{
				Date: "2026-03-22",
				Students: []MatchedStudent{
					{Name: "Alice", Class: "Math", QuotedText: "Did great", Confidence: 0.9},
					{Name: "Bob", Class: "Math", QuotedText: "Needs improvement", Confidence: 0.8},
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
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{result: "some transcript"},
		roster:        &stubRoster{},
		extractor:     &stubExtractor{err: io.ErrUnexpectedEOF},
		voiceNoteRepo: &VoiceNoteRepo{db: nil},
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}))

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	require.Error(t, err)

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	require.NoError(t, err)
	assert.Equal(t, JobStatusFailed, got.Status)
}

func TestProcessJob_NoteCreateFail(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "u1", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
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
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", QuotedText: "ok", Confidence: 0.9}},
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

	cls, err := classRepo.Create(t.Context(), "u1", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
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
				{Name: "Alice", Class: "Math", QuotedText: "ok", Confidence: 0.9},
				{Name: "Alice", Class: "WrongClass", QuotedText: "hallucinated", Confidence: 0.9},
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

	cls, err := classRepo.Create(t.Context(), "u1", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
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
				{Name: "Alice", Class: "Math", QuotedText: "ok", Confidence: 0.9},
				{Name: "Maybe", Class: "Math", QuotedText: "unsure", Confidence: 0.3},
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

	cls, err := classRepo.Create(t.Context(), "u1", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
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
				{Name: "Alice", Class: "Math", QuotedText: rawQuote, Confidence: 0.95},
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
