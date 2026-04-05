# Generic Queue + Voice Note Refactor

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Replace the hardcoded `UploadJob`/`UploadQueue`/`memQueue` with a Go-generics-based `MemQueue[T]`/`JobQueue[T]` infrastructure and a concrete `VoiceNoteJob` type. Rename `uploads` DB table to `voice_notes`. Enables clean addition of future async job types (e.g. report card examples) with full type safety.

**Architecture:** Introduce a `Keyed` constraint interface and generic `JobQueue[T]`/`MemQueue[T]`. Each job type gets its own queue instance and processor function. The voice note pipeline (transcribe→extract→create notes) is extracted into `processVoiceNote`, injected into a `MemQueue[VoiceNoteJob]` via closure. No `any` fields — full compile-time type safety.

**Tech Stack:** Go 1.22+ (generics), SQLite, existing worker pool pattern

---

### Task 1: Add migration to rename uploads table → voice_notes

**Files:**
- Create: `backend/sql/002_rename_uploads.sql`

**Step 1: Write the migration**

```sql
ALTER TABLE uploads RENAME TO voice_notes;

DROP INDEX IF EXISTS idx_uploads_user;
DROP INDEX IF EXISTS idx_uploads_cleanup;

CREATE INDEX IF NOT EXISTS idx_voice_notes_user ON voice_notes(user_id);
CREATE INDEX IF NOT EXISTS idx_voice_notes_cleanup ON voice_notes(processed_at)
    WHERE processed_at IS NOT NULL;
```

**Step 2: Run tests to verify migration applies**

`setupTestDB` runs all migrations, so the full suite validates this:

```bash
cd backend && go test -v -count=1 ./...
```

Expected: PASS.

**Step 3: Commit**

```bash
git add backend/sql/002_rename_uploads.sql
git commit -m "migrate: rename uploads table to voice_notes"
```

---

### Task 2: Rename UploadRepo → VoiceNoteRepo

**Files:**
- Rename: `backend/repo_upload.go` → `backend/repo_voice_note.go`
- Rename: `backend/upload_cleanup.go` → `backend/voice_note_cleanup.go`
- Modify: `backend/deps.go`
- Modify: `backend/upload.go`
- Modify: `backend/drive_import.go`
- Modify: `backend/upload_process.go`
- Modify: `backend/jobs_dismiss.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/testutil_test.go`
- Modify: `backend/integration_test.go`

**Step 1: Rename struct + file**

```bash
cd backend && git mv repo_upload.go repo_voice_note.go
```

In `repo_voice_note.go`:
- `UploadRepo` → `VoiceNoteRepo`
- `Upload` struct → `VoiceNote`
- All SQL: `uploads` → `voice_notes`
- All error messages/comments updated

```go
// VoiceNoteRepo provides CRUD operations for the voice_notes table.
type VoiceNoteRepo struct{ db *sql.DB }

// VoiceNote represents a row in the voice_notes table.
type VoiceNote struct {
	ID          int64   `json:"id"`
	UserID      string  `json:"userId"`
	FileName    string  `json:"fileName"`
	FilePath    string  `json:"filePath"`
	ProcessedAt *string `json:"processedAt,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

func (r *VoiceNoteRepo) Create(ctx context.Context, userID, fileName, filePath string) (VoiceNote, error) {
	var v VoiceNote
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO voice_notes (user_id, file_name, file_path) VALUES (?, ?, ?)
		RETURNING id, user_id, file_name, file_path, processed_at, created_at`,
		userID, fileName, filePath,
	).Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.CreatedAt)
	if err != nil {
		return VoiceNote{}, fmt.Errorf("create voice note: %w", err)
	}
	return v, nil
}

// Same pattern for GetByID, MarkProcessed, ListStale, Delete — update table name,
// type names, error prefixes.
```

**Step 2: Rename cleanup file**

```bash
cd backend && git mv upload_cleanup.go voice_note_cleanup.go
```

Update function names:
- `StartUploadCleanup` → `StartVoiceNoteCleanup`
- `cleanProcessedUploads` → `cleanProcessedVoiceNotes`
- Parameter: `repo *UploadRepo` → `repo *VoiceNoteRepo`

**Step 3: Update deps interface**

In `deps.go`:
- `GetUploadRepo() *UploadRepo` → `GetVoiceNoteRepo() *VoiceNoteRepo`
- `prodDeps` field: `uploadRepo *UploadRepo` → `voiceNoteRepo *VoiceNoteRepo`
- `NewProdDeps`: `uploadRepo: &UploadRepo{db: db}` → `voiceNoteRepo: &VoiceNoteRepo{db: db}`
- Accessor: `func (p *prodDeps) GetUploadRepo() *UploadRepo` → `func (p *prodDeps) GetVoiceNoteRepo() *VoiceNoteRepo`

**Step 4: Update all callers**

In each file, replace `serviceDeps.GetUploadRepo()` → `serviceDeps.GetVoiceNoteRepo()` and `d.GetUploadRepo()` → `d.GetVoiceNoteRepo()`:
- `upload.go` (Create call)
- `drive_import.go` (Create call)
- `upload_process.go` (MarkProcessed call)
- `jobs_dismiss.go` (MarkProcessed call)

In `cmd/server/main.go`:
- `d.GetUploadRepo()` → `d.GetVoiceNoteRepo()`
- `handler.StartUploadCleanup` → `handler.StartVoiceNoteCleanup`

In `testutil_test.go`:
- `uploadRepo *UploadRepo` → `voiceNoteRepo *VoiceNoteRepo` in `mockDepsAll`
- `GetUploadRepo()` → `GetVoiceNoteRepo()`

In `integration_test.go`: update any `UploadRepo` references.

**Step 5: Run tests**

```bash
cd backend && go test -v -count=1 ./...
```

Expected: PASS.

**Step 6: Lint + commit**

```bash
cd backend && make lint
git add -A backend/
git commit -m "refactor: rename UploadRepo → VoiceNoteRepo, uploads table → voice_notes"
```

---

### Task 3: Create generic queue infrastructure (alongside old queue)

New files — nothing references them yet. Old `memQueue`/`UploadQueue` still in use.

**Files:**
- Create: `backend/job_queue.go`
- Create: `backend/job_queue_mem.go`
- Create: `backend/job_queue_mem_test.go`

**Step 1: Define Keyed + JobQueue interfaces**

Create `backend/job_queue.go`:

```go
// job_queue.go defines the generic job queue interfaces used for async
// processing. The in-memory implementation lives in job_queue_mem.go.
package handler

import "context"

// Keyed is the constraint for job types stored in a JobQueue.
// Each job must provide a unique key and an owner identifier for listing.
type Keyed interface {
	JobKey() string
	OwnerID() string
}

// JobQueue abstracts typed job queue operations.
type JobQueue[T Keyed] interface {
	// Publish stores the job and dispatches it for async processing.
	// Caller must set status/state before calling Publish.
	Publish(ctx context.Context, job T) error
	// GetJob reads a single job by key.
	GetJob(ctx context.Context, key string) (*T, error)
	// UpdateJob writes the full job state back.
	UpdateJob(ctx context.Context, job T) error
	// ListJobs returns all jobs for the given owner.
	ListJobs(ctx context.Context, ownerID string) ([]T, error)
	// DeleteJob removes a job from the store.
	DeleteJob(ctx context.Context, key string) error
	// Close tears down the queue and stops workers.
	Close()
}
```

**Step 2: Implement MemQueue[T]**

Create `backend/job_queue_mem.go`:

```go
// job_queue_mem.go provides a generic in-memory JobQueue implementation
// backed by a map and a buffered channel with a worker pool.
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// ProcessFunc is called by queue workers to process a job identified by key.
// It receives the queue (for reading/updating job state) and the job key.
type ProcessFunc[T Keyed] func(ctx context.Context, q JobQueue[T], key string) error

// MemQueue is a generic in-memory job queue with a background worker pool.
type MemQueue[T Keyed] struct {
	mu      sync.RWMutex
	jobs    map[string]T
	work    chan string // job keys
	process ProcessFunc[T]
	cancel  context.CancelFunc
}

// NewMemQueue creates a MemQueue and starts worker goroutines.
// Pass a non-zero workers count (e.g. 4). The process function is called
// by workers for each dispatched job.
func NewMemQueue[T Keyed](process ProcessFunc[T], workers int) *MemQueue[T] {
	ctx, cancel := context.WithCancel(context.Background())
	q := &MemQueue[T]{
		jobs:    make(map[string]T),
		work:    make(chan string, 100),
		process: process,
		cancel:  cancel,
	}
	for i := 0; i < workers; i++ {
		go q.worker(ctx)
	}
	return q
}

func (q *MemQueue[T]) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-q.work:
			if ctx.Err() != nil {
				return
			}
			if err := q.process(ctx, q, key); err != nil {
				slog.Error("MemQueue worker: job failed", "key", key, "error", err)
			}
		}
	}
}

func (q *MemQueue[T]) Publish(_ context.Context, job T) error {
	key := job.JobKey()
	q.mu.Lock()
	q.jobs[key] = job
	q.mu.Unlock()

	select {
	case q.work <- key:
	default:
		return fmt.Errorf("MemQueue: work channel full, job %s dropped", key)
	}
	return nil
}

func (q *MemQueue[T]) GetJob(_ context.Context, key string) (*T, error) {
	q.mu.RLock()
	job, ok := q.jobs[key]
	q.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("job not found: %s", key)
	}
	return &job, nil
}

func (q *MemQueue[T]) UpdateJob(_ context.Context, job T) error {
	key := job.JobKey()
	q.mu.Lock()
	q.jobs[key] = job
	q.mu.Unlock()
	return nil
}

func (q *MemQueue[T]) ListJobs(_ context.Context, ownerID string) ([]T, error) {
	prefix := ownerID + "/"
	q.mu.RLock()
	defer q.mu.RUnlock()

	var jobs []T
	for k, j := range q.jobs {
		if strings.HasPrefix(k, prefix) {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *MemQueue[T]) DeleteJob(_ context.Context, key string) error {
	q.mu.Lock()
	delete(q.jobs, key)
	q.mu.Unlock()
	return nil
}

func (q *MemQueue[T]) Close() {
	q.cancel()
}
```

**Step 3: Write tests for generic MemQueue**

Create `backend/job_queue_mem_test.go`:

```go
package handler

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// testJob is a minimal Keyed implementation for generic queue tests.
type testJob struct {
	Owner  string
	ID     int64
	Status string
	Data   string
}

func (j testJob) JobKey() string  { return fmt.Sprintf("%s/%d", j.Owner, j.ID) }
func (j testJob) OwnerID() string { return j.Owner }

func TestGenericQueue_PublishAndGetJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1, Status: "queued", Data: "hello"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got, err := q.GetJob(ctx, "u1/1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Data != "hello" {
		t.Errorf("data = %q, want %q", got.Data, "hello")
	}
}

func TestGenericQueue_GetJob_NotFound(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	_, err := q.GetJob(context.Background(), "u1/999")
	if err == nil {
		t.Fatal("expected error for missing job")
	}
}

func TestGenericQueue_UpdateJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1, Status: "queued"}); err != nil {
		t.Fatal(err)
	}

	if err := q.UpdateJob(ctx, testJob{Owner: "u1", ID: 1, Status: "done", Data: "result"}); err != nil {
		t.Fatal(err)
	}

	got, _ := q.GetJob(ctx, "u1/1")
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.Data != "result" {
		t.Errorf("data = %q, want result", got.Data)
	}
}

func TestGenericQueue_ListJobs(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	q.Publish(ctx, testJob{Owner: "u1", ID: 1})
	q.Publish(ctx, testJob{Owner: "u1", ID: 2})
	q.Publish(ctx, testJob{Owner: "u2", ID: 3})

	jobs, err := q.ListJobs(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Errorf("got %d jobs for u1, want 2", len(jobs))
	}

	jobs2, _ := q.ListJobs(ctx, "u2")
	if len(jobs2) != 1 {
		t.Errorf("got %d jobs for u2, want 1", len(jobs2))
	}
}

func TestGenericQueue_ListJobs_Empty(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	jobs, err := q.ListJobs(context.Background(), "nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("got %d jobs, want 0", len(jobs))
	}
}

func TestGenericQueue_DeleteJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	q.Publish(ctx, testJob{Owner: "u1", ID: 1})
	if err := q.DeleteJob(ctx, "u1/1"); err != nil {
		t.Fatal(err)
	}
	_, err := q.GetJob(ctx, "u1/1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestGenericQueue_ChannelFull(t *testing.T) {
	q := &MemQueue[testJob]{
		jobs:   make(map[string]testJob),
		work:   make(chan string, 1),
		cancel: func() {},
	}
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1}); err != nil {
		t.Fatal(err)
	}
	err := q.Publish(ctx, testJob{Owner: "u1", ID: 2})
	if err == nil {
		t.Fatal("expected error when channel is full")
	}
}

func TestGenericQueue_WorkerProcessesJob(t *testing.T) {
	processed := make(chan string, 1)
	q := NewMemQueue[testJob](func(ctx context.Context, q JobQueue[testJob], key string) error {
		processed <- key
		return nil
	}, 1)
	defer q.Close()

	q.Publish(context.Background(), testJob{Owner: "u1", ID: 1, Status: "queued"})

	select {
	case key := <-processed:
		if key != "u1/1" {
			t.Errorf("processed key = %q, want u1/1", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
}

func TestGenericQueue_Close_StopsWorkers(t *testing.T) {
	q := NewMemQueue[testJob](nil, 2)
	q.Close()
}
```

**Step 4: Run tests**

```bash
cd backend && go test -run TestGenericQueue -v -count=1 ./...
```

Expected: PASS.

**Step 5: Lint + commit**

```bash
cd backend && make lint
git add backend/job_queue.go backend/job_queue_mem.go backend/job_queue_mem_test.go
git commit -m "feat: add generic MemQueue[T Keyed] job queue infrastructure"
```

---

### Task 4: Cutover — VoiceNoteJob + generic queue + extract processor

Replace `UploadJob`/`UploadQueue`/`memQueue` with `VoiceNoteJob`/`JobQueue[VoiceNoteJob]`/`MemQueue[VoiceNoteJob]`. Extract `processVoiceNote`. All changes compile together as one atomic step.

**Files:**
- Create: `backend/voice_note_job.go`
- Create: `backend/voice_note_process.go`
- Modify: `backend/deps.go`
- Modify: `backend/upload.go`
- Modify: `backend/drive_import.go`
- Modify: `backend/jobs_list.go`
- Modify: `backend/jobs_retry.go`
- Modify: `backend/jobs_dismiss.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/testutil_test.go`
- Rename: `backend/upload_process_test.go` → `backend/voice_note_process_test.go`
- Modify: `backend/jobs_list_test.go`
- Modify: `backend/jobs_retry_test.go`
- Modify: `backend/integration_test.go`
- Delete: `backend/upload_queue.go`
- Delete: `backend/mem_queue.go`
- Delete: `backend/upload_process.go`
- Delete: `backend/mem_queue_test.go`

**Step 1: Create VoiceNoteJob**

Create `backend/voice_note_job.go`:

```go
// voice_note_job.go defines the VoiceNoteJob type and status constants
// for async voice note processing (transcribe → extract → create notes).
package handler

import (
	"fmt"
	"time"
)

// Job status constants for voice note processing.
const (
	JobStatusQueued        = "queued"
	JobStatusTranscribing  = "transcribing"
	JobStatusExtracting    = "extracting"
	JobStatusCreatingNotes = "creating_notes"
	JobStatusDone          = "done"
	JobStatusFailed        = "failed"
)

// NoteLink pairs a student name with the ID of the created note.
type NoteLink struct {
	Name      string `json:"name"`
	NoteID    int64  `json:"noteId"`
	StudentID int64  `json:"studentId"`
	ClassName string `json:"className"`
}

// VoiceNoteJob represents an async voice note processing job.
type VoiceNoteJob struct {
	UserID    string     `json:"userId"`
	UploadID  int64      `json:"uploadId"`
	FilePath  string     `json:"filePath"`
	FileName  string     `json:"fileName"`
	MimeType  string     `json:"mimeType"`
	Source    string     `json:"source"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	NoteLinks []NoteLink `json:"noteLinks,omitempty"`
	Error     string     `json:"error,omitempty"`
	FailedAt  *time.Time `json:"failedAt,omitempty"`
}

// JobKey implements Keyed.
func (j VoiceNoteJob) JobKey() string { return fmt.Sprintf("%s/%d", j.UserID, j.UploadID) }

// OwnerID implements Keyed.
func (j VoiceNoteJob) OwnerID() string { return j.UserID }

// voiceNoteKey builds a job key from user ID and upload ID.
// Used by handlers that receive these values separately.
func voiceNoteKey(userID string, uploadID int64) string {
	return fmt.Sprintf("%s/%d", userID, uploadID)
}
```

**Step 2: Create processVoiceNote**

Create `backend/voice_note_process.go` — extracted from `upload_process.go`:

```go
// voice_note_process.go implements the voice note processing pipeline
// (transcribe → extract → create notes). Called by MemQueue workers.
package handler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Minimum extraction confidence to auto-create a note.
const autoCreateConfidenceThreshold = 0.5

// processVoiceNote runs the voice note pipeline for a single job.
// It is the ProcessFunc for the voice note MemQueue — receives the queue
// (for status updates) and the job key.
func processVoiceNote(ctx context.Context, d deps, q JobQueue[VoiceNoteJob], key string) error {
	log := loggerFromContext(ctx)

	job, err := q.GetJob(ctx, key)
	if err != nil {
		return fmt.Errorf("process voice note: get job: %w", err)
	}

	// Idempotency: only process jobs that are queued.
	if job.Status != JobStatusQueued {
		log.Info("process voice note: skipping non-queued job", "key", key, "status", job.Status)
		return nil
	}

	userID := job.UserID
	uploadID := job.UploadID

	// Helper to mark job as failed and return the error.
	fail := func(step string, err error) error {
		log.Error("process voice note failed", "step", step, "key", key, "error", err)
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		job.FailedAt = &now
		if updateErr := q.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process voice note: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process voice note: %s: %w", step, err)
	}

	// --- Step 1: Transcribe ---
	job.Status = JobStatusTranscribing
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fail("update status to transcribing", err)
	}

	audioFile, err := os.Open(job.FilePath)
	if err != nil {
		return fail("open audio file", err)
	}
	defer audioFile.Close()

	var whisperPrompt string
	roster := d.GetRoster(ctx, userID)
	names, err := roster.ClassNames(ctx)
	if err != nil {
		log.Warn("process voice note: could not read class names", "error", err)
	} else if len(names) > 0 {
		whisperPrompt = "Classes: " + strings.Join(names, ", ")
	}

	transcriber, err := d.GetTranscriber()
	if err != nil {
		return fail("init transcriber", err)
	}

	transcript, err := transcriber.Transcribe(ctx, job.FileName, audioFile, whisperPrompt)
	if err != nil {
		return fail("transcribe", err)
	}

	// --- Step 2: Extract ---
	job.Status = JobStatusExtracting
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fail("update status to extracting", err)
	}

	classes, err := roster.Students(ctx)
	if err != nil {
		log.Warn("process voice note: could not read students for extraction", "error", err)
	}

	extractor, err := d.GetExtractor()
	if err != nil {
		return fail("init extractor", err)
	}

	extractResult, err := extractor.Extract(ctx, ExtractRequest{
		Transcript: transcript,
		Classes:    classes,
	})
	if err != nil {
		return fail("extract", err)
	}

	// --- Step 3: Create notes ---
	job.Status = JobStatusCreatingNotes
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fail("update status to creating_notes", err)
	}

	noteCreator := d.GetNoteCreator()
	studentRepo := d.GetStudentRepo()

	var noteLinks []NoteLink
	for _, student := range extractResult.Students {
		if student.Confidence < autoCreateConfidenceThreshold {
			log.Info("process voice note: skipping low-confidence match",
				"student", student.Name, "confidence", student.Confidence)
			continue
		}

		studentID, err := studentRepo.FindByNameAndClass(ctx, student.Name, student.Class, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				log.Warn("process voice note: student not found in DB, skipping",
					"student", student.Name, "class", student.Class)
				continue
			}
			return fail("find student "+student.Name, err)
		}

		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			StudentID:   studentID,
			StudentName: student.Name,
			Summary:     student.Summary,
			Transcript:  transcript,
			Date:        extractResult.Date,
		})
		if err != nil {
			return fail("create note for "+student.Name, err)
		}
		noteLinks = append(noteLinks, NoteLink{
			Name: student.Name, NoteID: result.NoteID,
			StudentID: studentID, ClassName: student.Class,
		})
	}

	// --- Done ---
	voiceNoteRepo := d.GetVoiceNoteRepo()
	if err := voiceNoteRepo.MarkProcessed(ctx, uploadID); err != nil {
		log.Warn("process voice note: failed to mark voice note processed", "error", err)
	}

	job.Status = JobStatusDone
	job.NoteLinks = noteLinks
	job.Error = ""
	job.FailedAt = nil
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("process voice note: update status to done: %w", err)
	}

	log.Info("process voice note completed",
		"key", key, "user_id", userID, "upload_id", uploadID,
		"note_count", len(noteLinks))
	return nil
}
```

**Step 3: Update deps.go**

Replace `GetUploadQueue() (UploadQueue, error)` with `GetVoiceNoteQueue() (JobQueue[VoiceNoteJob], error)`.

Replace the singleton + init:

```go
// Voice note queue singleton, initialised at startup via InitVoiceNoteQueue.
var voiceNoteQueueInstance JobQueue[VoiceNoteJob]

// InitVoiceNoteQueue creates the in-memory voice note queue, starts worker
// goroutines, and stores it as the package-level singleton.
func InitVoiceNoteQueue(d deps, workers int) *MemQueue[VoiceNoteJob] {
	q := NewMemQueue[VoiceNoteJob](func(ctx context.Context, queue JobQueue[VoiceNoteJob], key string) error {
		return processVoiceNote(ctx, d, queue, key)
	}, workers)
	voiceNoteQueueInstance = q
	return q
}

func (p *prodDeps) GetVoiceNoteQueue() (JobQueue[VoiceNoteJob], error) {
	if voiceNoteQueueInstance == nil {
		return nil, fmt.Errorf("voice note queue not initialized — call InitVoiceNoteQueue first")
	}
	return voiceNoteQueueInstance, nil
}
```

Remove old `uploadQueueInstance`, `InitUploadQueue`, `GetUploadQueue`.

**Step 4: Update upload.go**

Replace the queue publish block:

```go
queue, err := serviceDeps.GetVoiceNoteQueue()
if err != nil {
	log.Warn("upload: queue unavailable, skipping async processing", "error", err)
} else {
	if err := queue.Publish(ctx, VoiceNoteJob{
		UserID:    userID,
		UploadID:  upload.ID,
		FilePath:  diskPath,
		FileName:  header.Filename,
		MimeType:  contentType,
		Source:    "upload",
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}); err != nil {
		log.Error("upload: failed to dispatch job", "error", err)
	}
}
```

**Step 5: Update drive_import.go**

Same pattern — `GetVoiceNoteQueue()`, publish `VoiceNoteJob` with `Source: "drive_import"`, `Status: JobStatusQueued`.

Note: the existing code has `Source: "drive-import"` (hyphen). Normalize to `"drive_import"` (underscore) for consistency.

**Step 6: Update jobs_list.go**

- `serviceDeps.GetUploadQueue()` → `serviceDeps.GetVoiceNoteQueue()`
- `[]UploadJob` → `[]VoiceNoteJob` in `JobListResponse` and sorting helper

```go
type JobListResponse struct {
	Active []VoiceNoteJob `json:"active"`
	Failed []VoiceNoteJob `json:"failed"`
	Done   []VoiceNoteJob `json:"done"`
}
```

**Step 7: Update jobs_retry.go**

- `serviceDeps.GetUploadQueue()` → `serviceDeps.GetVoiceNoteQueue()`
- Add `j.Status = JobStatusQueued` before `queue.Publish(ctx, j)` (generic queue doesn't set status)
- `j.UploadID` → `j.UploadID` (unchanged, for log messages)

**Step 8: Update jobs_dismiss.go**

- `serviceDeps.GetUploadQueue()` → `serviceDeps.GetVoiceNoteQueue()`
- `queue.GetJob(r.Context(), userID, uploadID)` → `queue.GetJob(r.Context(), voiceNoteKey(userID, uploadID))`
- `queue.DeleteJob(r.Context(), userID, uploadID)` → `queue.DeleteJob(r.Context(), voiceNoteKey(userID, uploadID))`
- `serviceDeps.GetUploadRepo()` → `serviceDeps.GetVoiceNoteRepo()` (if not already done in Task 2)

**Step 9: Update cmd/server/main.go**

```go
queue := handler.InitVoiceNoteQueue(d, 4)
defer queue.Close()
```

**Step 10: Update testutil_test.go**

Replace `stubUploadQueue` with:

```go
// stubVoiceNoteQueue implements JobQueue[VoiceNoteJob] for tests.
type stubVoiceNoteQueue struct {
	jobs      map[string]VoiceNoteJob
	published []VoiceNoteJob
}

func newStubVoiceNoteQueue() *stubVoiceNoteQueue {
	return &stubVoiceNoteQueue{jobs: make(map[string]VoiceNoteJob)}
}

func (q *stubVoiceNoteQueue) Publish(_ context.Context, job VoiceNoteJob) error {
	q.jobs[job.JobKey()] = job
	q.published = append(q.published, job)
	return nil
}

func (q *stubVoiceNoteQueue) GetJob(_ context.Context, key string) (*VoiceNoteJob, error) {
	job, ok := q.jobs[key]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", key)
	}
	return &job, nil
}

func (q *stubVoiceNoteQueue) UpdateJob(_ context.Context, job VoiceNoteJob) error {
	q.jobs[job.JobKey()] = job
	return nil
}

func (q *stubVoiceNoteQueue) ListJobs(_ context.Context, ownerID string) ([]VoiceNoteJob, error) {
	prefix := ownerID + "/"
	var jobs []VoiceNoteJob
	for k, j := range q.jobs {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *stubVoiceNoteQueue) DeleteJob(_ context.Context, key string) error {
	delete(q.jobs, key)
	return nil
}

func (q *stubVoiceNoteQueue) Close() {}
```

Update `mockDepsAll`:
- `uploadQueue UploadQueue` → `voiceNoteQueue JobQueue[VoiceNoteJob]`
- `uploadQueueErr error` → `voiceNoteQueueErr error`
- `GetUploadQueue()` → `GetVoiceNoteQueue()` returning `m.voiceNoteQueue, m.voiceNoteQueueErr`

Remove `newTestQueue` helper (or update it to return `*stubVoiceNoteQueue`).

**Step 11: Update process tests**

```bash
cd backend && git mv upload_process_test.go voice_note_process_test.go
```

Update all tests:
- `UploadJob{...}` → `VoiceNoteJob{..., Status: JobStatusQueued}`
- `newStubUploadQueue()` → `newStubVoiceNoteQueue()`
- `kvKey("u1", 1)` → `voiceNoteKey("u1", 1)` (for direct map access in stubs: `queue.jobs[voiceNoteKey(...)]`)
- `processUploadJob(ctx, d, "user1", 1)` → `processVoiceNote(ctx, d, queue, voiceNoteKey("user1", 1))`
- `d.uploadQueue = queue` → `d.voiceNoteQueue = queue`
- `d.uploadRepo = uploadRepo` → `d.voiceNoteRepo = voiceNoteRepo` (if not already done in Task 2)

Note: `processVoiceNote` takes the queue directly as a parameter, so tests pass the stub queue explicitly — no need to wire it through deps for the processor.

**Step 12: Update jobs_list_test.go, jobs_retry_test.go**

- `UploadJob{...}` → `VoiceNoteJob{..., Status: ...}`
- `kvKey(...)` → `voiceNoteKey(...)`
- `newStubUploadQueue()` → `newStubVoiceNoteQueue()`
- `uploadQueue: queue` → `voiceNoteQueue: queue` in `mockDepsAll`

**Step 13: Update integration_test.go**

Same pattern — replace all `UploadJob`, `uploadQueue`, `kvKey`, `processUploadJob` references.

**Step 14: Delete old files**

```bash
cd backend && git rm upload_queue.go mem_queue.go upload_process.go mem_queue_test.go
```

**Step 15: Run full test suite**

```bash
cd backend && go test -v -count=1 ./...
```

Expected: ALL PASS.

**Step 16: Lint + commit**

```bash
cd backend && make lint
git add -A backend/
git commit -m "refactor: replace UploadJob/UploadQueue with generic JobQueue + VoiceNoteJob"
```

---

### Task 5: Regenerate TypeScript types + update docs

**Files:**
- Regenerate: `frontend/src/api-types.gen.ts`
- Modify: `backend/ARCHITECTURE.md`

**Step 1: Regenerate TypeScript types**

```bash
cd backend && make generate
```

Verify `frontend/src/api-types.gen.ts` now has `VoiceNoteJob` instead of `UploadJob`, and `JobListResponse` uses it.

**Step 2: Check frontend compiles**

```bash
cd frontend && npm run build 2>&1 | head -30
```

If the frontend references `UploadJob` by name, update those imports to `VoiceNoteJob`. Grep for it:

```bash
cd frontend && grep -rn "UploadJob" src/
```

**Step 3: Update ARCHITECTURE.md**

Key sections to update:
- **Tables**: `uploads` → `voice_notes`
- **File-by-file reference**: remove `upload_queue.go`, `mem_queue.go`, `upload_process.go`; add `job_queue.go`, `job_queue_mem.go`, `voice_note_job.go`, `voice_note_process.go`; rename `repo_upload.go` → `repo_voice_note.go`, `upload_cleanup.go` → `voice_note_cleanup.go`
- **Key Interfaces table**: `UploadQueue` → `JobQueue[VoiceNoteJob]`, `memQueue` → `MemQueue[VoiceNoteJob]`
- **Async Upload Processing Pipeline**: update type names (`VoiceNoteJob`, `processVoiceNote`, `InitVoiceNoteQueue`)
- **Dependency Injection**: `GetUploadQueue()` → `GetVoiceNoteQueue()`, `GetUploadRepo()` → `GetVoiceNoteRepo()`

**Step 4: Commit**

```bash
git add frontend/src/api-types.gen.ts backend/ARCHITECTURE.md
git commit -m "docs: update TypeScript types and architecture for voice note refactor"
```

---

### 🛑 Manual Verification Checkpoint

1. Run full test suite: `cd backend && go test -v -count=1 ./...`
2. Run locally, upload a voice note, verify it processes correctly
3. Import from Drive, verify processing works
4. Check jobs list, retry, dismiss all work
5. Verify frontend renders job progress correctly

---

## Decisions

- **Generic queue via Go generics** — `MemQueue[T Keyed]` with `ProcessFunc[T]` injected at construction. Full type safety, no `any` fields.
- **Separate queues per job type** — future job types get their own `MemQueue` instance. No unified job listing needed.
- **`Keyed` constraint** — minimal: `JobKey() string` + `OwnerID() string`.
- **Queue does not set status** — callers set `Status = JobStatusQueued` before `Publish`. Keeps the generic queue status-agnostic.
- **Processor receives queue + key** — `ProcessFunc[T](ctx, q, key)`. Processor owns all lifecycle (idempotency check, intermediate statuses, done/fail marking).
- **DB table renamed** — `uploads` → `voice_notes`. `UploadRepo` → `VoiceNoteRepo`.
- **Source field preserved** — `"upload"` / `"drive_import"` kept as-is (origin info).
- **Cleanup goroutine** — stays wired to `VoiceNoteRepo`. Multi-type cleanup is out of scope.
