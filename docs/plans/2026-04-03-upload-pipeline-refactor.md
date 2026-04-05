# Upload Pipeline Refactor — Source-Based Dispatch

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Refactor `processUploadJob` so it dispatches to source-specific processors based on `UploadJob.Source`, enabling reuse for report example extraction without changing existing voice note behavior.

**Architecture:** Extract the current voice-note pipeline (transcribe→extract→create notes) into a dedicated processor function. Introduce a source-based dispatcher in `processUploadJob` that routes `"upload"` and `"drive_import"` to the voice note processor. Future sources (e.g. `"report_example"`) plug in by adding a case. Pure refactor — no behavior change.

**Tech Stack:** Go, existing memQueue/UploadQueue infrastructure

---

### Task 1: Extract voice note processor from `processUploadJob`

**Files:**
- Modify: `backend/upload_process.go`

**Step 1: Run existing tests to establish green baseline**

```bash
cd backend && go test -v -count=1 ./...
```

Expected: all PASS.

**Step 2: Extract voice note logic into `processVoiceNote`**

Move the body of `processUploadJob` (steps 1-3: transcribe, extract, create notes) into a new function:

```go
// processVoiceNote runs the voice note pipeline: transcribe → extract → create notes.
func processVoiceNote(ctx context.Context, d deps, job *UploadJob, queue UploadQueue) error {
	// ... all existing logic from processUploadJob steps 1-3 moves here ...
}
```

Then `processUploadJob` becomes a thin dispatcher:

```go
func processUploadJob(ctx context.Context, d deps, userID string, uploadID int64) error {
	log := loggerFromContext(ctx)

	queue, err := d.GetUploadQueue()
	if err != nil {
		return fmt.Errorf("process job: get queue: %w", err)
	}

	job, err := queue.GetJob(ctx, userID, uploadID)
	if err != nil {
		return fmt.Errorf("process job: get job: %w", err)
	}

	if job.Status != JobStatusQueued {
		log.Info("process job: skipping non-queued job", "user_id", userID, "upload_id", uploadID, "status", job.Status)
		return nil
	}

	// Helper to mark job as failed.
	fail := func(step string, err error) error {
		log.Error("process job failed", "step", step, "user_id", userID, "upload_id", uploadID, "error", err)
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		job.FailedAt = &now
		if updateErr := queue.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process job: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process job: %s: %w", step, err)
	}

	switch job.Source {
	case "upload", "drive_import":
		if err := processVoiceNote(ctx, d, job, queue); err != nil {
			return fail("voice_note", err)
		}
	default:
		return fail("dispatch", fmt.Errorf("unknown source: %s", job.Source))
	}

	// Mark upload as processed.
	uploadRepo := d.GetUploadRepo()
	if err := uploadRepo.MarkProcessed(ctx, uploadID); err != nil {
		log.Warn("process job: failed to mark upload processed", "error", err)
	}

	job.Status = JobStatusDone
	job.Error = ""
	job.FailedAt = nil
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("process job: update status to done: %w", err)
	}

	log.Info("process job completed",
		"user_id", userID, "upload_id", uploadID, "source", job.Source,
		"note_count", len(job.NoteLinks))
	return nil
}
```

Note: `processVoiceNote` should set intermediate statuses (transcribing, extracting, creating_notes) and populate `job.NoteLinks`, but should NOT set `JobStatusDone` or call `MarkProcessed` — that stays in the dispatcher.

**Step 3: Run tests — everything should still pass**

```bash
cd backend && go test -v -count=1 ./...
```

Expected: all PASS (pure refactor, no behavior change).

**Step 4: Lint**

```bash
cd backend && make lint
```

**Step 5: Commit**

```bash
git add backend/upload_process.go
git commit -m "refactor: extract processVoiceNote from processUploadJob dispatcher"
```

### Task 2: Rename `Source` constant for clarity

**Files:**
- Modify: `backend/upload.go` (change `"upload"` to `"voice_note"`)
- Modify: `backend/drive_import.go` (change `"drive_import"` to `"voice_note"`)
- Modify: `backend/upload_process.go` (update switch case)

**Step 1: Update source constants**

In `backend/upload.go`, change:
```go
Source: "upload",
```
to:
```go
Source: "voice_note",
```

In `backend/drive_import.go`, find where Source is set and change similarly.

In `backend/upload_process.go`, update the switch:
```go
case "voice_note":
    if err := processVoiceNote(ctx, d, job, queue); err != nil {
```

**Step 2: Run tests**

```bash
cd backend && go test -v -count=1 ./...
```

Expected: PASS. If any test checks the Source value, update accordingly.

**Step 3: Lint and commit**

```bash
cd backend && make lint
git add backend/upload.go backend/drive_import.go backend/upload_process.go
git commit -m "refactor: rename upload source to voice_note"
```

### Task 3: Add test for unknown source rejection

**Files:**
- Test: `backend/upload_process_test.go`

**Step 1: Write the failing test**

```go
func TestProcessUploadJob_UnknownSource(t *testing.T) {
	d := &mockDepsAll{ /* minimal stubs */ }
	withDeps(t, d)
	queue := newStubUploadQueue()
	d.uploadQueue = queue

	queue.jobs[kvKey("u1", 99)] = UploadJob{
		UserID:   "u1",
		UploadID: 99,
		Source:   "unknown_source",
		Status:   JobStatusQueued,
	}

	err := processUploadJob(context.Background(), d, "u1", 99)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
	if !strings.Contains(err.Error(), "unknown source") {
		t.Errorf("error = %q, want to contain 'unknown source'", err.Error())
	}

	// Job should be marked failed.
	job, _ := queue.GetJob(context.Background(), "u1", 99)
	if job.Status != JobStatusFailed {
		t.Errorf("status = %q, want failed", job.Status)
	}
}
```

**Step 2: Run test**

```bash
cd backend && go test -run TestProcessUploadJob_UnknownSource -v
```

Expected: PASS (dispatcher already handles this).

**Step 3: Commit**

```bash
git add backend/upload_process_test.go
git commit -m "test: add unknown source rejection test for upload dispatcher"
```

### 🛑 Manual Verification Checkpoint

1. Run full test suite: `cd backend && go test -v -count=1 ./...`
2. Run locally, upload a voice note, verify it processes correctly
3. Verify job list endpoint still works

This is a pure refactor — all existing behavior should be identical.

## Decisions
- `processVoiceNote` owns the transcribe→extract→notes steps and intermediate status updates
- `processUploadJob` owns job fetch, dispatch, done/fail marking, and `MarkProcessed`
- Both existing sources (`"upload"`, `"drive_import"`) renamed to `"voice_note"` since they do the same thing
- No new source types added in this PR — that's for the PDF extraction plan
