// integration_test.go exercises the voice-note pipeline end to end — publish,
// process, retry, list — against an in-memory queue and DB with stub
// transcriber/extractor/note-creator. Despite the TestIntegration_ prefix
// nothing here talks to a real LLM; the live-provider tests live in
// llm_live_test.go behind the `llm` build tag.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_PublishToNoteCreation(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "int-user", "Math", "")
	alice, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)
	bob, err := studentRepo.Create(t.Context(), cls.ID, "Bob")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio bytes"), 0o644))

	queue := newTestQueue(t)
	nc := &stubNoteCreator{
		results: []*CreateNoteResponse{
			{NoteID: 1},
			{NoteID: 2},
		},
	}

	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "Alice did great. Bob needs work."},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}, {Name: "Bob"}}}},
		},
		extractor: &stubExtractor{result: &ExtractResponse{
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "Did great", Confidence: 0.9},
				{Name: "Bob", ClassName: "Math · Mon", QuotedText: "Needs work", Confidence: 0.8},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()

	job := VoiceNoteJob{
		UserID:    "int-user",
		UploadID:  1,
		FilePath:  audioPath,
		FileName:  "2026-03-22-recording.m4a",
		MimeType:  "audio/mp4",
		Source:    "web",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}
	require.NoError(t, queue.Publish(ctx, job), "publish")

	got, err := queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	require.NoError(t, err, "get job after publish")
	assert.Equal(t, JobStatusQueued, got.Status, "status after publish")

	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1)), "process")

	got, err = queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	require.NoError(t, err, "get job after process")
	assert.Equal(t, JobStatusDone, got.Status)
	// Each link must point at the resolved student, with name and class in
	// their own fields — a count alone passes with them transposed.
	assert.Equal(t, []NoteLink{
		{Name: "Alice", NoteID: 1, StudentID: alice.ID, ClassName: "Math · Mon"},
		{Name: "Bob", NoteID: 2, StudentID: bob.ID, ClassName: "Math · Mon"},
	}, got.NoteLinks)
	require.Len(t, nc.calls, 2)
	assert.Equal(t, alice.ID, nc.calls[0].StudentID)
	assert.Equal(t, bob.ID, nc.calls[1].StudentID)
}

func TestIntegration_PublishToFailure(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	queue := newTestQueue(t)

	d := &mockDepsAll{
		transcriber:   &stubTranscriber{err: ErrNotFound},
		roster:        &stubRoster{},
		voiceNoteRepo: &VoiceNoteRepo{db: nil},
	}

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "int-user", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now(),
	}), "publish")

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1))
	require.Error(t, err)

	got, err := queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	require.NoError(t, err, "get job")
	assert.Equal(t, JobStatusFailed, got.Status)
	assert.NotNil(t, got.FailedAt, "failedAt should be set")
}

func TestIntegration_RetryAfterFailure(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "int-user", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio"), 0o644))

	queue := newTestQueue(t)
	failingTranscriber := &stubTranscriber{err: ErrNotFound}
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: failingTranscriber,
		roster:      &stubRoster{},
		extractor: &stubExtractor{result: &ExtractResponse{
			Students: []MatchedStudent{{Name: "Alice", ClassName: "Math · Mon", QuotedText: "ok", Confidence: 0.9}},
		}},
		noteCreator:    nc,
		voiceNoteQueue: queue,
		studentRepo:    studentRepo,
		voiceNoteRepo:  voiceNoteRepo,
	}

	old := serviceDeps
	serviceDeps = d
	t.Cleanup(func() { serviceDeps = old })

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID: "int-user", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now(),
	}), "publish")

	// First attempt fails.
	require.Error(t, processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1)), "expected error on first attempt")
	got, err := queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	require.NoError(t, err, "get job")
	assert.Equal(t, JobStatusFailed, got.Status)

	// Fix the transcriber.
	failingTranscriber.err = nil
	failingTranscriber.result = "transcript"

	// Retry via handler.
	req := httptest.NewRequest(http.MethodPost, "/jobs/retry", http.NoBody)
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "int-user"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()
	handleJobRetry(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "retry status; body = %s", rec.Body.String())
	var retryResp jobRetryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&retryResp), "decode retry resp")
	assert.Equal(t, 1, retryResp.RetriedCount)

	// Process the retried job.
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1)), "second process")

	got, err = queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	require.NoError(t, err, "get job after retry")
	assert.Equal(t, JobStatusDone, got.Status)
}

func TestIntegration_ListJobsDuringProcessing(t *testing.T) {
	queue := newTestQueue(t)
	ctx := context.Background()

	// Job 1: done.
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, Status: JobStatusQueued, CreatedAt: time.Now()}))
	doneJob, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	require.NoError(t, err)
	doneJob.Status = JobStatusDone
	doneJob.NoteLinks = []NoteLink{{Name: "Test Student", NoteID: 1, StudentID: 5, ClassName: "Math · Mon"}}
	require.NoError(t, queue.UpdateJob(ctx, *doneJob))

	// Job 2: failed.
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 2, Status: JobStatusQueued, CreatedAt: time.Now()}))
	failedJob, err := queue.GetJob(ctx, voiceNoteKey("u1", 2))
	require.NoError(t, err)
	now := time.Now()
	failedJob.Status = JobStatusFailed
	failedJob.Error = "boom"
	failedJob.FailedAt = &now
	require.NoError(t, queue.UpdateJob(ctx, *failedJob))

	// Job 3: done as well — two done, one failed, three active, so no two
	// buckets share a size and a swapped case body cannot go unnoticed.
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 3, Status: JobStatusQueued, CreatedAt: time.Now()}))
	doneJob2, err := queue.GetJob(ctx, voiceNoteKey("u1", 3))
	require.NoError(t, err)
	doneJob2.Status = JobStatusDone
	require.NoError(t, queue.UpdateJob(ctx, *doneJob2))

	// Jobs 4-6: still in flight.
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 4, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 5, Status: JobStatusQueued, CreatedAt: time.Now()}))
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 6, Status: JobStatusQueued, CreatedAt: time.Now()}))

	old := serviceDeps
	serviceDeps = &mockDepsAll{voiceNoteQueue: queue}
	t.Cleanup(func() { serviceDeps = old })

	req := httptest.NewRequest(http.MethodGet, "/jobs", http.NoBody)
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "u1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()
	handleJobList(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var resp JobListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp), "decode")
	assert.ElementsMatch(t, []int64{4, 5, 6}, uploadIDs(resp.Active))
	assert.ElementsMatch(t, []int64{2}, uploadIDs(resp.Failed))
	assert.ElementsMatch(t, []int64{1, 3}, uploadIDs(resp.Done))
}
