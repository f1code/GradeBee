package handler

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestProcessJob_HappyPath(t *testing.T) {
	queue := newStubUploadQueue()
	nc := &stubNoteCreator{
		results: []*CreateNoteResponse{
			{DocID: "doc1", DocURL: "https://docs.google.com/1"},
			{DocID: "doc2", DocURL: "https://docs.google.com/2"},
		},
	}
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("fake audio")),
			fileName:     "recording.m4a",
		},
		transcriber: &stubTranscriber{result: "Alice did great today. Bob needs improvement."},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []classGroup{{Name: "Math", Students: []student{{Name: "Alice"}, {Name: "Bob"}}}},
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
		noteCreator:    nc,
		uploadQueue:    queue,
		metadata:       &gradeBeeMetadata{NotesID: "notes-folder-id"},
	}

	ctx := context.Background()
	job := UploadJob{
		UserID:    "user1",
		FileID:    "file1",
		FileName:  "recording.m4a",
		CreatedAt: time.Now(),
	}
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatal(err)
	}

	if err := processUploadJob(ctx, d, "user1", "file1"); err != nil {
		t.Fatalf("processUploadJob: %v", err)
	}

	got, err := queue.GetJob(ctx, "user1", "file1"); if err != nil { t.Fatal(err) }
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want %q", got.Status, JobStatusDone)
	}
	if len(got.NoteURLs) != 2 {
		t.Errorf("noteUrls = %d, want 2", len(got.NoteURLs))
	}
	if len(nc.calls) != 2 {
		t.Errorf("note creator calls = %d, want 2", len(nc.calls))
	}
	if nc.calls[0].StudentName != "Alice" {
		t.Errorf("first note student = %q, want Alice", nc.calls[0].StudentName)
	}
}

func TestProcessJob_TranscribeFail(t *testing.T) {
	queue := newStubUploadQueue()
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("fake audio")),
			fileName:     "recording.m4a",
		},
		transcriber: &stubTranscriber{err: io.ErrUnexpectedEOF},
		roster:      &stubRoster{},
		uploadQueue: queue,
		metadata:    &gradeBeeMetadata{NotesID: "notes-id"},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f1", CreatedAt: time.Now()}); err != nil { t.Fatal(err) }

	err := processUploadJob(ctx, d, "u1", "f1")
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "u1", "f1"); if err != nil { t.Fatal(err) }
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
	if !strings.Contains(got.Error, "transcribe") {
		t.Errorf("error = %q, want to contain 'transcribe'", got.Error)
	}
}

func TestProcessJob_ExtractFail(t *testing.T) {
	queue := newStubUploadQueue()
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("audio")),
			fileName:     "test.m4a",
		},
		transcriber: &stubTranscriber{result: "some transcript"},
		roster:      &stubRoster{},
		extractor:   &stubExtractor{err: io.ErrUnexpectedEOF},
		uploadQueue: queue,
		metadata:    &gradeBeeMetadata{NotesID: "notes-id"},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f1", CreatedAt: time.Now()}); err != nil { t.Fatal(err) }

	err := processUploadJob(ctx, d, "u1", "f1")
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "u1", "f1"); if err != nil { t.Fatal(err) }
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
	if !strings.Contains(got.Error, "extract") {
		t.Errorf("error = %q, want to contain 'extract'", got.Error)
	}
}

func TestProcessJob_NoteCreateFail(t *testing.T) {
	queue := newStubUploadQueue()
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("audio")),
			fileName:     "test.m4a",
		},
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date:     "2026-01-01",
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", Summary: "ok", Confidence: 0.9}},
		}},
		noteCreator: &stubNoteCreator{err: io.ErrUnexpectedEOF},
		uploadQueue: queue,
		metadata:    &gradeBeeMetadata{NotesID: "notes-id"},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f1", CreatedAt: time.Now()}); err != nil { t.Fatal(err) }

	err := processUploadJob(ctx, d, "u1", "f1")
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, "u1", "f1"); if err != nil { t.Fatal(err) }
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
}

func TestProcessJob_AlreadyProcessed(t *testing.T) {
	queue := newStubUploadQueue()
	d := &mockDepsAll{uploadQueue: queue}

	ctx := context.Background()
	// Seed a job already done.
	queue.jobs[kvKey("u1", "f1")] = UploadJob{
		UserID: "u1", FileID: "f1", Status: JobStatusDone,
	}

	err := processUploadJob(ctx, d, "u1", "f1")
	if err != nil {
		t.Fatalf("expected no error for already-processed job, got: %v", err)
	}

	got, err := queue.GetJob(ctx, "u1", "f1"); if err != nil { t.Fatal(err) }
	if got.Status != JobStatusDone {
		t.Errorf("status changed to %q, should remain done", got.Status)
	}
}

func TestProcessJob_MissingMetadata(t *testing.T) {
	queue := newStubUploadQueue()
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("audio")),
			fileName:     "test.m4a",
		},
		transcriber: &stubTranscriber{result: "transcript"},
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date:     "2026-01-01",
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", Summary: "ok", Confidence: 0.9}},
		}},
		uploadQueue: queue,
		metadata:    nil, // no metadata
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f1", CreatedAt: time.Now()}); err != nil { t.Fatal(err) }

	err := processUploadJob(ctx, d, "u1", "f1")
	if err == nil {
		t.Fatal("expected error for missing metadata")
	}

	got, err := queue.GetJob(ctx, "u1", "f1"); if err != nil { t.Fatal(err) }
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
}

func TestProcessJob_LowConfidenceSkipped(t *testing.T) {
	queue := newStubUploadQueue()
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(strings.NewReader("audio")),
			fileName:     "test.m4a",
		},
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
		metadata:    &gradeBeeMetadata{NotesID: "notes-id"},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, UploadJob{UserID: "u1", FileID: "f1", CreatedAt: time.Now()}); err != nil { t.Fatal(err) }

	if err := processUploadJob(ctx, d, "u1", "f1"); err != nil {
		t.Fatal(err)
	}

	if len(nc.calls) != 1 {
		t.Errorf("note creator calls = %d, want 1 (low confidence skipped)", len(nc.calls))
	}
	if nc.calls[0].StudentName != "Alice" {
		t.Errorf("created note for %q, want Alice", nc.calls[0].StudentName)
	}
}
