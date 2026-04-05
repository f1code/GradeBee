# Review Fix-ups: Naming Consistency + Status Contract

**Goal:** Address code review findings from the generic queue refactor. All changes are naming consistency fixes plus one defensive check.

**Branch:** `refactor/generic-queue-voice-note` (continue on same branch)

---

### Task 1: Add defensive status warning in MemQueue.Publish

**Files:** `backend/job_queue_mem.go`

**Step 1:** Add a `log/slog` warning in `MemQueue.Publish` when the job's key suggests it should have a status set but doesn't. Since `MemQueue` is generic and status-agnostic, the best approach is to add a comment documenting the contract on the `Publish` method in `job_queue.go`:

In `backend/job_queue.go`, update the `Publish` doc comment:
```go
	// Publish stores the job and dispatches it for async processing.
	// Caller must set status/state before calling Publish — the queue
	// does not modify job fields. If status is not set, the processor's
	// idempotency check may silently skip the job.
	Publish(ctx context.Context, job T) error
```

**Step 2:** Run tests: `cd backend && go test -count=1 ./...`

---

### Task 2: Fix stale variable/field/test names

**Files:**
- `backend/jobs_dismiss.go` — rename `uploadRepo` → `voiceNoteRepo`
- `backend/cmd/server/main.go` — rename `uploadRepo` → `voiceNoteRepo`
- `backend/repo_test.go` — rename `repos.uploads` field → `voiceNotes`, rename `TestUploadRepo_CRUD` → `TestVoiceNoteRepo_CRUD`, update all `r.uploads.` → `r.voiceNotes.`

**Step 1:** In `backend/jobs_dismiss.go`, rename the local variable:
```go
// before:
uploadRepo := serviceDeps.GetVoiceNoteRepo()
// after:
voiceNoteRepo := serviceDeps.GetVoiceNoteRepo()
```
And update the two references to `uploadRepo` later in the function to `voiceNoteRepo`.

**Step 2:** In `backend/cmd/server/main.go`, rename:
```go
// before:
uploadRepo := d.GetVoiceNoteRepo()
go handler.StartVoiceNoteCleanup(ctx, uploadRepo, ...)
// after:
voiceNoteRepo := d.GetVoiceNoteRepo()
go handler.StartVoiceNoteCleanup(ctx, voiceNoteRepo, ...)
```

**Step 3:** In `backend/repo_test.go`:
- Rename struct field `uploads *VoiceNoteRepo` → `voiceNotes *VoiceNoteRepo`
- Update initialization: `uploads: &VoiceNoteRepo{db: db}` → `voiceNotes: &VoiceNoteRepo{db: db}`
- Rename `TestUploadRepo_CRUD` → `TestVoiceNoteRepo_CRUD`
- Replace all `r.uploads.` → `r.voiceNotes.` in that test

**Step 4:** Run tests + lint:
```bash
cd backend && go test -count=1 ./... && make lint
```

---

### Task 3: Make VoiceNoteJob.JobKey call voiceNoteKey

**Files:** `backend/voice_note_job.go`

**Step 1:** Update `JobKey` to delegate:
```go
func (j VoiceNoteJob) JobKey() string { return voiceNoteKey(j.UserID, j.UploadID) }
```

**Step 2:** Run tests: `cd backend && go test -count=1 ./...`

---

### Task 4: Remove unused uploadsDir parameter from StartVoiceNoteCleanup

**Files:**
- `backend/voice_note_cleanup.go`
- `backend/cmd/server/main.go`

**Step 1:** Remove `uploadsDir string` from `StartVoiceNoteCleanup` signature:
```go
func StartVoiceNoteCleanup(ctx context.Context, repo *VoiceNoteRepo, retention, interval time.Duration) {
```

**Step 2:** Update caller in `cmd/server/main.go`:
```go
go handler.StartVoiceNoteCleanup(ctx, voiceNoteRepo, time.Duration(retentionHours)*time.Hour, 1*time.Hour)
```

**Step 3:** Run tests + lint:
```bash
cd backend && go test -count=1 ./... && make lint
```

---

### Task 5: Commit all fixes

```bash
cd backend && git add -A . && git commit -m "fix: address review — naming consistency, status contract docs, remove unused param"
```
