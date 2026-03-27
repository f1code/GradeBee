# Phase 2 Completion: Frontend Async Upload + Backend Cleanup

## Goal

Migrate the web frontend from the synchronous upload→transcribe→extract→confirm→save flow to the async model: upload returns immediately, backend processes in background, frontend polls `GET /jobs` for status. Remove dead sync endpoints from backend.

## Current State

- **Backend:** `POST /upload` and `POST /drive-import` already dispatch jobs to `memQueue` and return immediately. `GET /jobs` and `POST /jobs/retry` exist and work. However `/transcribe`, `/extract`, `/notes` endpoints still exist and are called by the frontend.
- **Frontend:** `AudioUpload.tsx` calls `transcribeAudio` → `extractFromTranscript` → shows `NoteConfirmation` → `createNotes` sequentially. User must keep the page open through the entire pipeline. No job polling.

## Proposed Changes

### 1. Backend: Remove sync endpoints

**`backend/handler.go`** — Remove route registrations for `/transcribe`, `/extract`, `/notes`.

**`backend/transcribe.go`** — Delete file (the `handleTranscribe` HTTP handler). Keep `transcriber.go` (interface + impl used by `upload_process.go`).

**`backend/extract_handler.go`** — Delete file. Keep `extract.go` (interface + impl used by `upload_process.go`).

**`backend/notes_handler.go`** — Delete file. Keep `notes.go` (interface + impl used by `upload_process.go`).

**`backend/ARCHITECTURE.md`** — Remove `/transcribe`, `/extract`, `/notes` from routing table.

### 2. Frontend: Simplify `api.ts`

**`frontend/src/api.ts`**

- Remove `transcribeAudio()`, `extractFromTranscript()`, `createNotes()` functions
- Remove `ExtractResult`, `MatchedStudent`, `CreateNotesRequest`, `NoteResult` types (no longer needed client-side)
- Add types:
  ```ts
  interface UploadJob {
    fileId: string
    fileName: string
    status: 'queued' | 'transcribing' | 'extracting' | 'creating_notes' | 'done' | 'failed'
    error?: string
    noteIds?: string[]
    createdAt: string
  }
  interface JobListResponse {
    active: UploadJob[]
    failed: UploadJob[]
    done: UploadJob[]
  }
  ```
- Add `fetchJobs(getToken): Promise<JobListResponse>`
- Add `retryFailedJobs(getToken): Promise<void>`

### 3. Frontend: Rewrite `AudioUpload.tsx`

**`frontend/src/components/AudioUpload.tsx`**

Simplify to only handle file selection + upload. After upload succeeds, show a brief "Uploaded! Processing in background." toast/message and reset to idle. No more `transcribing`/`extracting`/`confirming`/`saving` states.

Reduced status type: `'idle' | 'uploading' | 'error'`

Remove all imports/references to `NoteConfirmation`, `transcribeAudio`, `extractFromTranscript`, `createNotes`.

Keep: file validation, drag & drop, Drive import via picker, mobile layout.

### 4. Frontend: New `JobStatus.tsx` component

**`frontend/src/components/JobStatus.tsx`** — New file.

Polls `GET /jobs` every 3s while there are active jobs (falls back to 15s when all idle/done). Shows three sections:

- **Active jobs** — spinner + status label per job (e.g. "Transcribing lesson-march-25.m4a...")
- **Failed jobs** — error message + "Retry All" button (calls `POST /jobs/retry`)
- **Recent done** — last N completed jobs with note count, "new" badge for jobs completed since last poll. Links to notes folder (or individual docs if noteIds available — need `GET /jobs` to return doc URLs, see open question).

Design: card-style per the design system (`--chalk` bg, `12px` radius). Use `HoneycombSpinner` for active state. Fraunces headings, Source Sans body.

Placement: render in `App.tsx` between `StudentList` and `AudioUpload` on the Notes tab.

### 5. Frontend: Delete `NoteConfirmation.tsx`

**`frontend/src/components/NoteConfirmation.tsx`** — Delete file.

**`frontend/src/components/__tests__/NoteConfirmation.test.tsx`** — Delete file.

### 6. Frontend: Update tests

**`frontend/src/components/__tests__/AudioUpload.test.tsx`** — Rewrite to match new simplified flow (upload → success message, no transcribe/extract/confirm steps). Mock `fetchJobs` if needed.

Add **`frontend/src/components/__tests__/JobStatus.test.tsx`** — Test polling behavior, status rendering, retry button, badge logic.

### 7. Update `App.tsx`

**`frontend/src/App.tsx`** — Import and render `JobStatus` in the Notes tab, between `StudentList` and `AudioUpload`. Update `HintBanner` copy to reflect new flow ("Upload audio — GradeBee processes it in the background and creates notes automatically.").

## Decisions

1. **Note links:** Construct doc URLs backend-side (`https://docs.google.com/document/d/{id}/edit`) and store them on the job. No API call needed — URLs are deterministic from the doc ID. Change `upload_process.go` to store URLs (not just IDs) on `UploadJob`, and update `UploadJob.NoteIDs` → `NoteURLs []string` (or add a parallel field). `GET /jobs` returns URLs directly.

2. **Job persistence:** In-memory is fine. Jobs survive page refreshes — `memQueue` stores by `userID/fileID` and `GET /jobs` filters by the authenticated user. Jobs only lost on server restart, which is acceptable.

3. **Review step:** Add a "Review" affordance on done jobs in `JobStatus`. Show created notes with a delete button per note, so users can remove low-confidence/unwanted notes post-creation.

## File Change Summary

| File | Action |
|------|--------|
| `backend/handler.go` | Remove 3 route registrations |
| `backend/transcribe.go` | Delete |
| `backend/extract_handler.go` | Delete |
| `backend/notes_handler.go` | Delete |
| `backend/upload_queue.go` | Add `NoteURLs` field (or replace `NoteIDs`) |
| `backend/upload_process.go` | Store doc URLs on job after note creation |
| `backend/ARCHITECTURE.md` | Update routing table |
| `frontend/src/api.ts` | Remove 3 functions + types, add 2 functions + types |
| `frontend/src/components/AudioUpload.tsx` | Simplify (remove sync pipeline) |
| `frontend/src/components/JobStatus.tsx` | New file |
| `frontend/src/components/NoteConfirmation.tsx` | Delete |
| `frontend/src/components/__tests__/NoteConfirmation.test.tsx` | Delete |
| `frontend/src/components/__tests__/AudioUpload.test.tsx` | Rewrite |
| `frontend/src/components/__tests__/JobStatus.test.tsx` | New file |
| `frontend/src/App.tsx` | Add JobStatus, update hint copy |
