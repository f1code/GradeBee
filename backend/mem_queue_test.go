package handler

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func mustPublish(t *testing.T, q *memQueue, ctx context.Context, job UploadJob) {
	t.Helper()
	if err := q.Publish(ctx, job); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func mustGetJob(t *testing.T, q *memQueue, ctx context.Context, userID, fileID string) *UploadJob {
	t.Helper()
	got, err := q.GetJob(ctx, userID, fileID)
	if err != nil {
		t.Fatalf("GetJob(%s, %s): %v", userID, fileID, err)
	}
	return got
}

func TestMemQueue_PublishAndGetJob(t *testing.T) {
	q := NewMemQueue(nil, 0)
	defer q.Close()

	ctx := context.Background()
	mustPublish(t, q, ctx, UploadJob{
		UserID:   "u1",
		FileID:   "f1",
		FileName: "test.pdf",
		MimeType: "application/pdf",
		Source:   "upload",
	})

	got := mustGetJob(t, q, ctx, "u1", "f1")
	if got.Status != JobStatusQueued {
		t.Errorf("status = %q, want %q", got.Status, JobStatusQueued)
	}
	if got.FileName != "test.pdf" {
		t.Errorf("fileName = %q, want %q", got.FileName, "test.pdf")
	}
	if got.Source != "upload" {
		t.Errorf("source = %q, want %q", got.Source, "upload")
	}
}

func TestMemQueue_GetJob_NotFound(t *testing.T) {
	q := NewMemQueue(nil, 0)
	defer q.Close()

	_, err := q.GetJob(context.Background(), "u1", "missing")
	if err == nil {
		t.Fatal("expected error for missing job")
	}
}

func TestMemQueue_UpdateJob(t *testing.T) {
	q := NewMemQueue(nil, 0)
	defer q.Close()

	ctx := context.Background()
	mustPublish(t, q, ctx, UploadJob{UserID: "u1", FileID: "f1"})

	now := time.Now()
	if err := q.UpdateJob(ctx, UploadJob{
		UserID:   "u1",
		FileID:   "f1",
		Status:   JobStatusFailed,
		Error:    "something broke",
		FailedAt: &now,
		NoteIDs:  []string{"n1"},
	}); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	got := mustGetJob(t, q, ctx, "u1", "f1")
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, JobStatusFailed)
	}
	if got.Error != "something broke" {
		t.Errorf("error = %q, want %q", got.Error, "something broke")
	}
	if got.FailedAt == nil {
		t.Error("failedAt should be set")
	}
	if len(got.NoteIDs) != 1 || got.NoteIDs[0] != "n1" {
		t.Errorf("noteIDs = %v, want [n1]", got.NoteIDs)
	}
}

func TestMemQueue_ListJobs(t *testing.T) {
	q := NewMemQueue(nil, 0)
	defer q.Close()

	ctx := context.Background()
	mustPublish(t, q, ctx, UploadJob{UserID: "u1", FileID: "f1", FileName: "a.pdf"})
	mustPublish(t, q, ctx, UploadJob{UserID: "u1", FileID: "f2", FileName: "b.pdf"})
	mustPublish(t, q, ctx, UploadJob{UserID: "u2", FileID: "f3", FileName: "c.pdf"})

	jobs, err := q.ListJobs(ctx, "u1")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("got %d jobs for u1, want 2", len(jobs))
	}

	jobs2, err := q.ListJobs(ctx, "u2")
	if err != nil {
		t.Fatalf("ListJobs u2: %v", err)
	}
	if len(jobs2) != 1 {
		t.Errorf("got %d jobs for u2, want 1", len(jobs2))
	}

	for _, j := range jobs {
		if j.Status != JobStatusQueued {
			t.Errorf("job %s status = %q, want %q", j.FileID, j.Status, JobStatusQueued)
		}
	}
}

func TestMemQueue_ListJobs_Empty(t *testing.T) {
	q := NewMemQueue(nil, 0)
	defer q.Close()

	jobs, err := q.ListJobs(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("got %d jobs, want 0", len(jobs))
	}
}

func TestMemQueue_WorkerProcessesJob(t *testing.T) {
	mock := &mockDepsAll{
		googleSvcForUserErr: fmt.Errorf("no google services"),
	}

	q := NewMemQueue(mock, 1)
	defer q.Close()

	// Wire queue into mock so processUploadJob can read/update jobs.
	mock.uploadQueue = q

	ctx := context.Background()
	mustPublish(t, q, ctx, UploadJob{
		UserID:    "u1",
		FileID:    "f1",
		FileName:  "lecture.mp3",
		MimeType:  "audio/mpeg",
		Source:    "upload",
		CreatedAt: time.Now(),
	})

	// Poll until worker processes job (status leaves "queued").
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			got := mustGetJob(t, q, ctx, "u1", "f1")
			t.Fatalf("timed out; job status = %q", got.Status)
		default:
		}

		got := mustGetJob(t, q, ctx, "u1", "f1")
		if got.Status == JobStatusFailed {
			if got.Error == "" {
				t.Error("expected error message on failed job")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMemQueue_ChannelFull(t *testing.T) {
	q := &memQueue{
		jobs:   make(map[string]UploadJob),
		work:   make(chan jobRef, 1),
		cancel: func() {},
	}
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, UploadJob{UserID: "u1", FileID: "f1"}); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	err := q.Publish(ctx, UploadJob{UserID: "u1", FileID: "f2"})
	if err == nil {
		t.Fatal("expected error when channel is full")
	}
}

func TestMemQueue_Close_StopsWorkers(t *testing.T) {
	q := NewMemQueue(nil, 2)
	q.Close()

	// After Close, no panic on publish.
	if err := q.Publish(context.Background(), UploadJob{UserID: "u1", FileID: "f1"}); err != nil {
		// Channel send may fail, that's fine.
		t.Logf("Publish after Close: %v", err)
	}
}
