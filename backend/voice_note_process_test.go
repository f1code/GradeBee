package handler

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessJob_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	// Seed class + students.
	cls, err := classRepo.Create(t.Context(), "user1", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Bob"); err != nil {
		t.Fatal(err)
	}

	// Write a temp audio file.
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}

	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("user1", 1)); err != nil {
		t.Fatalf("processVoiceNote: %v", err)
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("user1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want %q", got.Status, JobStatusDone)
	}
	if len(got.NoteLinks) != 2 {
		t.Errorf("noteLinks = %d, want 2", len(got.NoteLinks))
	}
	if len(nc.calls) != 2 {
		t.Errorf("note creator calls = %d, want 2", len(nc.calls))
	}
}

func TestProcessJob_TranscribeFail(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{err: io.ErrUnexpectedEOF},
		roster:        &stubRoster{},
		voiceNoteRepo: &VoiceNoteRepo{db: nil}, // won't be called on failure
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
	if !strings.Contains(got.Error, "transcribe") {
		t.Errorf("error = %q, want to contain 'transcribe'", got.Error)
	}
}

func TestProcessJob_ExtractFail(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{
		transcriber:   &stubTranscriber{result: "some transcript"},
		roster:        &stubRoster{},
		extractor:     &stubExtractor{err: io.ErrUnexpectedEOF},
		voiceNoteRepo: &VoiceNoteRepo{db: nil},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
}

func TestProcessJob_NoteCreateFail(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "u1", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	err = processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	if err == nil {
		t.Fatal("expected error")
	}

	got, gErr := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if gErr != nil {
		t.Fatal(gErr)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
}

func TestProcessJob_AlreadyProcessed(t *testing.T) {
	queue := newStubVoiceNoteQueue()
	d := &mockDepsAll{}

	ctx := context.Background()
	queue.jobs[voiceNoteKey("u1", 1)] = VoiceNoteJob{
		UserID: "u1", UploadID: 1, Status: JobStatusDone,
	}

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatalf("expected no error for already-processed job, got: %v", err)
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status changed to %q, should remain done", got.Status)
	}
}

func TestProcessJob_WrongClassSkipped(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "u1", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)); err != nil {
		t.Fatalf("processVoiceNote should succeed despite wrong class: %v", err)
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want %q", got.Status, JobStatusDone)
	}
	if len(nc.calls) != 1 {
		t.Errorf("note creator calls = %d, want 1 (wrong class skipped)", len(nc.calls))
	}
}

func TestProcessJob_LowConfidenceSkipped(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "u1", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Maybe"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)); err != nil {
		t.Fatal(err)
	}

	if len(nc.calls) != 1 {
		t.Errorf("note creator calls = %d, want 1 (low confidence skipped)", len(nc.calls))
	}
}

// TestProcessJob_QuotedTextPassedToNoteCreator verifies that QuotedText from
// extraction flows through to CreateNoteRequest without modification.
func TestProcessJob_QuotedTextPassedToNoteCreator(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "u1", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)); err != nil {
		t.Fatalf("processVoiceNote: %v", err)
	}

	if len(nc.calls) != 1 {
		t.Fatalf("expected 1 note creation call, got %d", len(nc.calls))
	}
	if nc.calls[0].QuotedText != rawQuote {
		t.Errorf("QuotedText not passed through.\nGot:  %s\nWant: %s", nc.calls[0].QuotedText, rawQuote)
	}
}
