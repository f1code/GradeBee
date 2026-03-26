# Mobile Share-to-GradeBee App

## Goal

Build a minimal React Native (Expo) app that registers as an OS share target on iOS and Android. When a user shares an audio file, it uploads to the GradeBee backend and is processed asynchronously via the in-memory upload queue — the same pipeline used by web uploads. Notes are auto-created (no manual confirmation step); users review/edit/delete from the existing notes list on the web.

## High-Level Flow (unified for web + mobile)

1. **Upload** — audio file reaches backend (web upload, Drive import, or mobile share)
2. Backend saves file to Drive `uploads/`, creates an in-memory `UploadJob` (`queued`), dispatches to `memQueue`
3. Returns `{ fileId, fileName, status: "queued" }` immediately
4. Worker goroutine picks up job: transcribe (Whisper) → extract (GPT) → auto-create notes (Google Docs) → `done`
5. On failure → job status set to `failed` with error
6. Web frontend polls `GET /jobs` for status + shows "new" badges on auto-created notes
7. User reviews notes in existing notes list, can edit or delete any

### What changes for web users

The current synchronous wizard (upload → wait for transcribe → wait for extract → confirm → save) is **replaced** by:

- Upload returns instantly, progress shown via status polling
- No confirmation step — notes are auto-created
- New notes appear in the notes list with a "new" badge (clears on first view)
- User can edit or delete any note from the list (undo for bad extractions)

This is a UX improvement: no more waiting through 30-60s of spinners. Teachers can upload multiple recordings quickly and review the results later.

### Endpoints removed

| Method | Path | Reason |
|--------|------|--------|
| POST | `/transcribe` | Moved into async worker pipeline |
| POST | `/extract` | Moved into async worker pipeline |
| POST | `/notes` | Moved into async worker pipeline (auto-create) |

### Frontend changes

- **`AudioUpload.tsx`**: simplify to upload-only + status indicator. Remove transcribe/extract/confirm steps.
- **Remove `NoteConfirmation.tsx`** (no longer needed)
- **Notes list**: add "new" badge for notes created since last visit. Add delete button per note.

## Architecture: In-Memory Upload Queue

### Why in-memory queue

- Zero external dependencies — no managed message broker needed
- Simple Go channels + worker goroutines handle concurrency
- Job state tracked in a map keyed by `userId/fileId` — no external DB needed
- Worker pool size configurable at startup (default: 4 goroutines)
- Graceful shutdown on SIGINT/SIGTERM drains in-flight jobs
- Good enough for single-instance deployment (current setup)

### Design principle: queue for dispatch, map for state

The buffered channel handles work dispatch. All job state queries (list, filter by user, check status) go through the in-memory job map.

### UploadJob lifecycle

```
POST /upload or /drive-import or /share-upload
        │  Saves file to Drive, creates UploadJob in map (status: "queued")
        │  Sends job to buffered channel
        │
        ▼
  memQueue worker goroutine
        │  Picks job from channel
        │
        ▼
  processUploadJob
        │
        ├─ Idempotency check: skip if job status ≠ "queued"
        │
        ├─ Step 1: Transcribe (status → "transcribing")
        │    Download audio from Drive → OpenAI Whisper
        │    Whisper prompt seeded with class names from roster
        │
        ├─ Step 2: Extract (status → "extracting")
        │    Send transcript + student roster to GPT
        │    → per-student observations (name, class, summary, confidence)
        │
        ├─ Step 3: Create Notes (status → "creating_notes")
        │    For each student with confidence ≥ 0.5:
        │      Create a Google Doc in the user's notes folder
        │
        └─ Done (status → "done", noteIDs stored on job)
```

On failure at any step, the job status is set to `"failed"` with the error message.

### UploadJob struct

```go
type UploadJob struct {
    UserID    string     `json:"userId"`
    FileID    string     `json:"fileId"`
    FileName  string     `json:"fileName"`
    MimeType  string     `json:"mimeType"`
    Source    string     `json:"source"` // "web" or "mobile"
    Status    string     `json:"status"`
    CreatedAt time.Time  `json:"createdAt"`
    NoteIDs   []string   `json:"noteIds,omitempty"`
    Error     string     `json:"error,omitempty"`
    FailedAt  *time.Time `json:"failedAt,omitempty"`
}
```

### UploadQueue interface

```go
type UploadQueue interface {
    Publish(ctx context.Context, job UploadJob) error
    UpdateStatus(ctx context.Context, userID, fileID, status string, err error) error
    ListJobs(ctx context.Context, userID string) ([]UploadJob, error)
    GetJob(ctx context.Context, userID, fileID string) (*UploadJob, error)
}
```

### Retry flow

`POST /jobs/retry` lists jobs for the user, filters for `status: "failed"`, resets each to `status: "queued"`, and republishes to the channel.

## Proposed Changes

### 1. Backend: async upload pipeline (ALREADY IMPLEMENTED)

The following are already in place and **do not need changes**:

**`backend/upload_queue.go`** — `UploadQueue` interface + `UploadJob` type + status constants

**`backend/mem_queue.go`** — in-memory `UploadQueue` implementation with worker pool (`memQueue`)

- Buffered channel for job dispatch
- Map keyed by `userId/fileId` for job state
- Configurable worker count (default: 4)
- Graceful shutdown support

**`backend/upload_process.go`** — `processUploadJob` pipeline

- Gets user's Google OAuth token from Clerk via `getGoogleOAuthToken(ctx, job.UserID)`
- Runs pipeline: transcribe → extract → create notes (uses `Transcriber`, `Extractor`, `NoteCreator`)
- Updates job status at each stage for progress visibility
- On success: status → `done` with `noteIDs`
- On failure: status → `failed` with error

**`backend/upload.go`** — `POST /upload`

- After saving file to Drive `uploads/`, dispatches job to `memQueue`
- Returns `{ fileId, fileName, status: "queued" }` immediately

**`backend/drive_import.go`** — `POST /drive-import`

- After copying file to `uploads/`, dispatches job to `memQueue`
- Returns `{ fileId, fileName, status: "queued" }` immediately

**`backend/jobs_list.go`** — `GET /jobs`

- Lists user's jobs from in-memory map
- Returns jobs grouped by status (active, failed, done)

**`backend/jobs_retry.go`** — `POST /jobs/retry`

- Filters user's jobs for `status: "failed"`, resets to `queued`, republishes to channel
- Returns `{ retriedCount: N }`

**`backend/cmd/server/main.go`** — calls `InitUploadQueue(ServiceDeps(), 4)` at startup

### 2. Backend: mobile share endpoint

**`backend/share_upload.go`** — `POST /share-upload`

- Accept audio files via multipart/form-data (25MB limit)
- Add ISO date prefix to filename
- Save to user's Drive `uploads/` folder (same as web)
- Dispatch to `memQueue` with `source: "mobile"`
- Return `{ fileId, fileName, status: "queued" }`

| Method | Path | Auth | Handler | Description |
|--------|------|------|---------|-------------|
| POST | `/share-upload` | Yes | `handleShareUpload` | Mobile share upload + enqueue |

### 3. Environment variables

No new environment variables needed — the in-memory queue requires no external services.

### 4. Frontend changes

**`frontend/src/components/AudioUpload.tsx`** — simplify

- Remove transcribe → extract → confirm flow
- Upload triggers `POST /upload` or `POST /drive-import`, both return immediately
- Show inline status: "Queued" → "Transcribing..." → "Extracting..." → "Creating notes..." → "Done ✓"
- Poll `GET /jobs` for progress updates (or just the relevant job)
- On done: show link to created notes
- On failure: show error + retry button

**`frontend/src/components/NoteConfirmation.tsx`** — remove

**`frontend/src/components/JobStatus.tsx`** — new

- Small status indicator component for a single job
- Shows current pipeline stage with progress animation
- Used inline in AudioUpload after upload completes

**Notes list** — minor additions

- "New" badge on notes created by auto-processing (since user's last visit)
- Delete button per note (Google Doc deletion via existing Drive API)

### 5. Mobile app: `mobile/`

**Framework:** Expo (managed workflow)

**Key dependencies:**
- `expo` + `expo-share-intent` — receive share intents
- `@clerk/clerk-expo` + `expo-secure-store` — authentication
- `expo-file-system` — read shared file for upload

**File structure:**
```
mobile/
├── app.json                  # Expo config (share target registration)
├── package.json
├── App.tsx                   # Root: Clerk provider + navigation
├── src/
│   ├── auth/
│   │   └── ClerkProvider.tsx # Clerk setup with expo-secure-store
│   ├── screens/
│   │   ├── LoginScreen.tsx   # Sign-in (shown once)
│   │   ├── ShareScreen.tsx   # Receives shared file, confirms, uploads
│   │   └── QueueScreen.tsx   # Shows active + failed jobs, retry button
│   ├── api/
│   │   ├── upload.ts         # POST /share-upload
│   │   ├── jobs.ts           # GET /jobs
│   │   └── retry.ts          # POST /jobs/retry
│   └── components/
│       ├── JobList.tsx       # Renders active/failed job lists
│       └── StatusBadge.tsx   # "queued" / "transcribing" / "failed" badge
```

**QueueScreen.tsx:**
- Polls `GET /jobs` on focus (or pull-to-refresh)
- Shows two sections: "Processing" (active jobs) and "Failed" (with error messages)
- "Retry All" button calls `POST /jobs/retry`, then refreshes
- Each failed job shows filename, error, and timestamp

**Share target registration:**
- **Android:** Expo config plugin adds `<intent-filter>` for `ACTION_SEND` with `audio/*` MIME type
- **iOS:** Expo config plugin adds Share Extension via `expo-share-intent` (filtered to audio)

**Auth flow:**
- Clerk Expo SDK with `expo-secure-store` as token cache
- User signs in once; session persists
- On share intent, if no session → show login, then upload

### 6. Expo build & distribution

- EAS Build for iOS/Android binaries
- TestFlight (iOS) + internal track (Android) for testing

## Drive Folder Structure (unchanged)

```
GradeBee/
├── uploads/              ← audio files (web + mobile, with ISO date prefix)
├── notes/                ← auto-created Google Doc notes
├── reports/
├── report-examples/
└── ClassSetup
```

No separate `shares/` folder — mobile uploads go to the same `uploads/` as web.

## ✅ Validated: Clerk OAuth token works offline

Confirmed that `getGoogleOAuthToken(ctx, userID)` returns a valid, working Google Drive token when called server-side without an active user session. Clerk auto-refreshes tokens on demand. **No changes needed** — background worker processing can call `getGoogleOAuthToken` for any user at any time.

## Open Questions

1. ~~What should processing produce?~~ → Auto-create notes (no confirmation step). Users edit/delete from notes list.
2. ~~What file types to accept?~~ → Audio files only. Share intent MIME filter set to `audio/*`.
3. ~~Should the app support manual file picking?~~ → No, share intent only.
4. ~~File naming in Drive?~~ → ISO date prefix (e.g. `2026-03-22-recording.m4a`).
5. ~~Scaleway NATS trigger format~~ → No longer applicable; using in-memory queue with worker goroutines.
6. ~~HARD BLOCKER: Clerk OAuth token offline access~~ → Verified, works fine.

## Effort Estimate

| Task | Estimate |
|------|----------|
| ~~BLOCKER: verify Clerk OAuth token offline access~~ | ~~1 hour~~ ✅ Done |
| ~~Backend: async queue + worker pool (`upload_queue.go`, `mem_queue.go`)~~ | ✅ Done |
| ~~Backend: modify `POST /upload` + `POST /drive-import` to dispatch async jobs~~ | ✅ Done |
| ~~Backend: `processUploadJob` pipeline (transcribe → extract → notes)~~ | ✅ Done |
| ~~Backend: `GET /jobs` + `POST /jobs/retry`~~ | ✅ Done |
| Backend: `POST /share-upload` (mobile endpoint) | 1-2 hours |
| Backend: remove `/transcribe`, `/extract`, `/notes` endpoints | 1 hour |
| Frontend: simplify AudioUpload to upload-only + job status polling | 2-3 hours |
| Frontend: remove NoteConfirmation, add JobStatus component | 1-2 hours |
| Frontend: "new" badge + delete on notes list | 1-2 hours |
| Mobile: Expo project setup + Clerk auth | 2-3 hours |
| Mobile: Share intent handling + upload UI | 3-4 hours |
| Mobile: iOS Share Extension config | 2-3 hours |
| Mobile: QueueScreen (active + failed + retry) | 3-4 hours |
| Testing on devices | 2-3 hours |
| EAS Build setup + TestFlight/Play Store | 2-3 hours |
| **Total remaining** | **~3 days** |
