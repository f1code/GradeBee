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
	uploadRepo := &UploadRepo{db: db}

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

	queue := newStubUploadQueue()
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
					{Name: "Alice", Class: "Math", Summary: "Did great", Confidence: 0.9},
					{Name: "Bob", Class: "Math", Summary: "Needs improvement", Confidence: 0.8},
				},
			},
		},
		noteCreator: nc,
		uploadQueue: queue,
		studentRepo: studentRepo,
		uploadRepo:  uploadRepo,
	}

	ctx := context.Background()
	job := UploadJob{
		UserID:    "user1",
		UploadID:  1,
		FilePath:  audioPath,
		FileName:  "recording.m4a",
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}

	if err := processUploadJob(ctx, d, "user1", 1); err != nil {
		t.Fatalf("processUploadJob: %v", err)
	}

	got, err := queue.GetJob(ctx, "user1", 1)
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

	queue := newStubUploadQueue()
	d := &mockDepsAll{
		transcriber: &stubTranscriber{err: io.ErrUnexpectedEOF},
		roster:      &stubRoster{},
		uploadQueue: queue,
		uploadRepo:  &UploadRepo{db: nil}, // won't be called on failure
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 1, FilePath: audioPath, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	err := processUploadJob(ctx, d, "u1", 1)
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "u1", 1)
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

	queue := newStubUploadQueue()
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "some transcript"},
		roster:      &stubRoster{},
		extractor:   &stubExtractor{err: io.ErrUnexpectedEOF},
		uploadQueue: queue,
		uploadRepo:  &UploadRepo{db: nil},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 1, FilePath: audioPath, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	err := processUploadJob(ctx, d, "u1", 1)
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "u1", 1)
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
	uploadRepo := &UploadRepo{db: db}

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

	queue := newStubUploadQueue()
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date:     "2026-01-01",
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", Summary: "ok", Confidence: 0.9}},
		}},
		noteCreator: &stubNoteCreator{err: io.ErrUnexpectedEOF},
		uploadQueue: queue,
		studentRepo: studentRepo,
		uploadRepo:  uploadRepo,
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 1, FilePath: audioPath, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	err = processUploadJob(ctx, d, "u1", 1)
	if err == nil {
		t.Fatal("expected error")
	}

	got, gErr := queue.GetJob(ctx, "u1", 1)
	if gErr != nil {
		t.Fatal(gErr)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
}

func TestProcessJob_AlreadyProcessed(t *testing.T) {
	queue := newStubUploadQueue()
	d := &mockDepsAll{uploadQueue: queue}

	ctx := context.Background()
	queue.jobs[kvKey("u1", 1)] = UploadJob{
		UserID: "u1", UploadID: 1, Status: JobStatusDone,
	}

	err := processUploadJob(ctx, d, "u1", 1)
	if err != nil {
		t.Fatalf("expected no error for already-processed job, got: %v", err)
	}

	got, err := queue.GetJob(ctx, "u1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status changed to %q, should remain done", got.Status)
	}
}

func TestProcessJob_LowConfidenceSkipped(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	uploadRepo := &UploadRepo{db: db}

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

	queue := newStubUploadQueue()
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-01-01",
			Students: []MatchedStudent{
				{Name: "Alice", Class: "Math", Summary: "ok", Confidence: 0.9},
				{Name: "Maybe", Class: "Math", Summary: "unsure", Confidence: 0.3},
			},
		}},
		noteCreator: nc,
		uploadQueue: queue,
		studentRepo: studentRepo,
		uploadRepo:  uploadRepo,
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", UploadID: 1, FilePath: audioPath, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := processUploadJob(ctx, d, "u1", 1); err != nil {
		t.Fatal(err)
	}

	if len(nc.calls) != 1 {
		t.Errorf("note creator calls = %d, want 1 (low confidence skipped)", len(nc.calls))
	}
}
