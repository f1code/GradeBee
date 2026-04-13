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
)

func TestIntegration_PublishToNoteCreation(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "int-user", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Bob"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("fake audio bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := queue.Publish(ctx, job); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	if err != nil {
		t.Fatalf("get job after publish: %v", err)
	}
	if got.Status != JobStatusQueued {
		t.Fatalf("status after publish = %q, want queued", got.Status)
	}

	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1)); err != nil {
		t.Fatalf("process: %v", err)
	}

	got, err = queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	if err != nil {
		t.Fatalf("get job after process: %v", err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want done", got.Status)
	}
	if len(got.NoteLinks) != 2 {
		t.Errorf("noteLinks = %v, want 2 items", got.NoteLinks)
	}
	if len(nc.calls) != 2 {
		t.Errorf("note creator calls = %d, want 2", len(nc.calls))
	}
}

func TestIntegration_PublishToFailure(t *testing.T) {
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newTestQueue(t)

	d := &mockDepsAll{
		transcriber:   &stubTranscriber{err: ErrNotFound},
		roster:        &stubRoster{},
		voiceNoteRepo: &VoiceNoteRepo{db: nil},
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, VoiceNoteJob{
		UserID: "int-user", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	err := processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1))
	if err == nil {
		t.Fatal("expected error")
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.FailedAt == nil {
		t.Error("failedAt should be set")
	}
}

func TestIntegration_RetryAfterFailure(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "int-user", "Math")
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
	if err := queue.Publish(ctx, VoiceNoteJob{
		UserID: "int-user", UploadID: 1, FilePath: audioPath, Status: JobStatusQueued, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// First attempt fails.
	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1)); err == nil {
		t.Fatal("expected error on first attempt")
	}
	got, err := queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}

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

	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var retryResp jobRetryResponse
	if err := json.NewDecoder(rec.Body).Decode(&retryResp); err != nil {
		t.Fatalf("decode retry resp: %v", err)
	}
	if retryResp.RetriedCount != 1 {
		t.Errorf("retriedCount = %d, want 1", retryResp.RetriedCount)
	}

	// Process the retried job.
	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("int-user", 1)); err != nil {
		t.Fatalf("second process: %v", err)
	}

	got, err = queue.GetJob(ctx, voiceNoteKey("int-user", 1))
	if err != nil {
		t.Fatalf("get job after retry: %v", err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status after retry = %q, want done", got.Status)
	}
}

func TestIntegration_ListJobsDuringProcessing(t *testing.T) {
	queue := newTestQueue(t)
	ctx := context.Background()

	// Job 1: done.
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 1, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	doneJob, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	doneJob.Status = JobStatusDone
	doneJob.NoteLinks = []NoteLink{{Name: "Test Student", NoteID: 1, StudentID: 5, ClassName: "Math"}}
	if err := queue.UpdateJob(ctx, *doneJob); err != nil {
		t.Fatal(err)
	}

	// Job 2: failed.
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 2, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	failedJob, err := queue.GetJob(ctx, voiceNoteKey("u1", 2))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	failedJob.Status = JobStatusFailed
	failedJob.Error = "boom"
	failedJob.FailedAt = &now
	if err := queue.UpdateJob(ctx, *failedJob); err != nil {
		t.Fatal(err)
	}

	// Job 3: still queued.
	if err := queue.Publish(ctx, VoiceNoteJob{UserID: "u1", UploadID: 3, Status: JobStatusQueued, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp JobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Active) != 1 {
		t.Errorf("active = %d, want 1", len(resp.Active))
	}
	if len(resp.Failed) != 1 {
		t.Errorf("failed = %d, want 1", len(resp.Failed))
	}
	if len(resp.Done) != 1 {
		t.Errorf("done = %d, want 1", len(resp.Done))
	}
}

func TestIntegration_UpdateReportExample(t *testing.T) {
	db := setupTestDB(t)
	exampleRepo := &ReportExampleRepo{db: db}

	// Create an example first
	ex, err := exampleRepo.Create(t.Context(), "user1", "original.txt", "original content")
	if err != nil {
		t.Fatal(err)
	}

	store := newDBExampleStore(exampleRepo)
	old := serviceDeps
	serviceDeps = &mockDepsAll{exampleStore: store}
	t.Cleanup(func() { serviceDeps = old })

	// Update via full Handle router
	body, err := json.Marshal(map[string]string{"name": "updated.txt", "content": "updated content"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/report-examples/%d", ex.ID), bytes.NewReader(body))
	rctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	req = req.WithContext(rctx)
	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result ReportExample
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "updated.txt" {
		t.Errorf("name = %q, want updated.txt", result.Name)
	}
}

// llmExtractor creates a gptExtractor, skipping if OPENAI_API_KEY is not set.
func llmExtractor(t *testing.T) Extractor {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set, skipping LLM integration test")
	}
	e, err := newGPTExtractor()
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) != 1 {
		t.Fatalf("expected 1 student, got %d: %+v", len(result.Students), result.Students)
	}
	if result.Students[0].Name != "Alice Johnson" {
		t.Errorf("name = %q, want Alice Johnson", result.Students[0].Name)
	}
	if result.Students[0].Class != "Math 101" {
		t.Errorf("class = %q, want Math 101", result.Students[0].Class)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) < 2 {
		t.Fatalf("expected at least 2 students, got %d: %+v", len(result.Students), result.Students)
	}

	found := map[string]string{}
	for _, s := range result.Students {
		found[s.Name] = s.Class
	}
	if found["Bob Smith"] != "Math 101" {
		t.Errorf("Bob Smith class = %q, want Math 101", found["Bob Smith"])
	}
	if found["Diana Lee"] != "Science 202" {
		t.Errorf("Diana Lee class = %q, want Science 202", found["Diana Lee"])
	}
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
	if err != nil {
		t.Fatal(err)
	}

	// Tommy Wilson is not in any roster class. The extractor should return no students
	// (or possibly empty results). It must NOT invent a class name.
	validClasses := map[string]bool{"Math 101": true, "Science 202": true}
	for _, s := range result.Students {
		if !validClasses[s.Class] {
			t.Errorf("student %q assigned to invalid class %q", s.Name, s.Class)
		}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) < 1 {
		t.Fatalf("expected at least 1 student, got %d", len(result.Students))
	}
	var found bool
	for _, s := range result.Students {
		if s.Name == "Alexander Hamilton" {
			found = true
			if s.Class != "English 101" {
				t.Errorf("class = %q, want English 101", s.Class)
			}
			break
		}
	}
	if !found {
		t.Errorf("Alexander Hamilton not found in results: %+v", result.Students)
	}
}
