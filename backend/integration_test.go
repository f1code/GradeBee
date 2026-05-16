package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	cls, err := classRepo.Create(t.Context(), "int-user", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Bob")
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
			Date: "2026-03-22",
			Students: []MatchedStudent{
				{Name: "Alice", Class: "Math", QuotedText: "Did great", Confidence: 0.9},
				{Name: "Bob", Class: "Math", QuotedText: "Needs work", Confidence: 0.8},
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
	assert.Len(t, got.NoteLinks, 2)
	assert.Len(t, nc.calls, 2)
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

	cls, err := classRepo.Create(t.Context(), "int-user", "Math", "")
	require.NoError(t, err)
	_, err = studentRepo.Create(t.Context(), cls.ID, "Alice")
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
			Date:     "2026-01-01",
			Students: []MatchedStudent{{Name: "Alice", Class: "Math", QuotedText: "ok", Confidence: 0.9}},
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
	doneJob.NoteLinks = []NoteLink{{Name: "Test Student", NoteID: 1, StudentID: 5, ClassName: "Math"}}
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

	// Job 3: still queued.
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 3, Status: JobStatusQueued, CreatedAt: time.Now()}))

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
	assert.Len(t, resp.Active, 1)
	assert.Len(t, resp.Failed, 1)
	assert.Len(t, resp.Done, 1)
}

func TestIntegration_UpdateReportExample(t *testing.T) {
	db := setupTestDB(t)
	exampleRepo := &ReportExampleRepo{db: db}

	// Create an example first
	ex, err := exampleRepo.Create(t.Context(), "user1", "original.txt", "original content")
	require.NoError(t, err)

	store := newDBExampleStore(exampleRepo)
	old := serviceDeps
	serviceDeps = &mockDepsAll{exampleStore: store}
	t.Cleanup(func() { serviceDeps = old })

	// Update via full Handle router
	body, err := json.Marshal(map[string]string{"name": "updated.txt", "content": "updated content"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/report-examples/%d", ex.ID), bytes.NewReader(body))
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "want 200, got %d: %s", rec.Code, rec.Body.String())

	var result ReportExample
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	assert.Equal(t, "updated.txt", result.Name)
}

// llmExtractor creates a gptExtractor, skipping if OPENAI_API_KEY is not set.
func llmExtractor(t *testing.T) Extractor {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set, skipping LLM integration test")
	}
	e, err := newGPTExtractor()
	require.NoError(t, err)
	return e
}

func TestLLM_SingleStudentCorrectClass(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Alice Johnson demonstrated excellent problem-solving skills on today's algebra quiz. She scored 95% and helped her classmates understand the quadratic formula.",
		Classes:    classes,
	})
	require.NoError(t, err)
	require.Len(t, result.Students, 1, "got %+v", result.Students)
	assert.Equal(t, "Alice Johnson", result.Students[0].Name)
	assert.Equal(t, "Math 101", result.Students[0].Class)
}

func TestLLM_MultiStudentDifferentClasses(t *testing.T) {
	ext := llmExtractor(t)
	// Bob appears in both rosters — the LLM must use transcript context to pick the right class.
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Bob Smith"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Today I observed two students. In Math 101, Bob Smith was very engaged during the fractions lesson and volunteered to solve problems on the board. In Science 202, Diana Lee conducted her chemistry experiment carefully and wrote detailed lab notes.",
		Classes:    classes,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Students), 2, "got %+v", result.Students)

	found := map[string]string{}
	for _, s := range result.Students {
		found[s.Name] = s.Class
	}
	assert.Equal(t, "Math 101", found["Bob Smith"])
	assert.Equal(t, "Science 202", found["Diana Lee"])
}

func TestLLM_UnknownClassSkipped(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Report card for Tommy Wilson, Art 303. Tommy shows great creativity in his paintings and participates actively in class discussions about art history.",
		Classes:    classes,
	})
	require.NoError(t, err)

	// Tommy Wilson is not in any roster class. The extractor should return no students
	// (or possibly empty results). It must NOT invent a class name.
	validClasses := map[string]bool{"Math 101": true, "Science 202": true}
	for _, s := range result.Students {
		assert.True(t, validClasses[s.Class], "student %q assigned to invalid class %q", s.Name, s.Class)
	}
}

func TestLLM_PartialNameMatch(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "English 101", Students: []ClassStudent{{Name: "Alexander Hamilton"}, {Name: "Elizabeth Bennet"}}},
		{Name: "History 201", Students: []ClassStudent{{Name: "Theodore Roosevelt"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Alex Hamilton wrote an outstanding essay on democracy today. His arguments were well-structured and his writing has improved significantly this semester.",
		Classes:    classes,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Students), 1)

	var found bool
	for _, s := range result.Students {
		if s.Name == "Alexander Hamilton" {
			found = true
			assert.Equal(t, "English 101", s.Class)
			break
		}
	}
	assert.True(t, found, "Alexander Hamilton not found in results: %+v", result.Students)
}
