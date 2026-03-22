# Mobile Share-to-GradeBee App

## Goal

Build a minimal React Native (Expo) app that registers as an OS share target on iOS and Android. When a user shares a file from any app, it uploads to the GradeBee backend which saves it to the user's Google Drive. Shared files are processed asynchronously via NATS JetStream, with visibility into pending/failed jobs from the mobile app.

## High-Level Flow

1. User shares a file from any app → OS shows GradeBee in share sheet
2. App authenticates via Clerk (session persisted after first sign-in)
3. App POSTs file to backend `/share-upload`
4. Backend saves file to Drive `shares/` folder, writes job to KV (`queued`), publishes to NATS stream
5. App gets back `{ fileId, fileName, status: "queued" }` immediately
6. NATS consumer triggers processing function: updates KV → `processing`, transcribes audio / OCRs images, updates KV → `done`
7. On failure (after 3 retries) → KV updated to `failed` with error
8. App can view job status via `GET /shares` (reads KV) and retry failed ones via `POST /shares/retry`

## Architecture: NATS JetStream (Stream + KV)

### Why NATS

- Scaleway offers **managed NATS** (Messaging & Queuing) — no infra to run
- JetStream gives durable streams, consumer groups, ack/nack, redelivery
- JetStream **KV** provides a lightweight queryable store for job state — no external DB needed
- Scaleway serverless functions can be triggered by NATS messages natively
- Clean separation: upload is fast, processing is async

### Design principle: Stream for dispatch, KV for state

The stream handles work dispatch only — a single subject, no per-user filtering needed. All job state queries (list, filter by user, check status) go through the KV bucket, which supports key prefix listing natively.

### Stream: dispatch

```
Stream: SHARES
  Subject: shares.process    ← jobs to process

Message payload:
{
  "userId":    "user_xxx",
  "fileId":    "drive_file_id"
}
```

The stream message is intentionally minimal — just enough to identify the job. Full metadata lives in KV.

Stream config:
- **Max age:** 7 days (auto-cleanup of processed messages)
- **Duplicate window:** 5 min (deduplication via `Nats-Msg-Id` header = `fileId`)

### KV bucket: `SHARE_JOBS`

Key format: `<userId>/<fileId>`

```json
{
  "userId":    "user_xxx",
  "fileId":    "drive_file_id",
  "fileName":  "photo.jpg",
  "mimeType":  "image/jpeg",
  "status":    "queued|processing|done|failed",
  "createdAt": "2026-03-22T10:00:00Z",
  "error":     "",
  "failedAt":  null
}
```

KV config:
- **TTL:** 30 days (completed/failed jobs auto-expire)
- **History:** 1 (only latest state needed)

### Consumer: `shares-processor`

- **Pull consumer** on `shares.process` (or push consumer triggering serverless function)
- Ack timeout: 5 min (generous for Whisper/GPT calls)
- **Max deliver: 3** with backoff (30s, 2min, 5min) — handles transient failures automatically
- On receive: update KV status → `processing`
- On success: update KV status → `done`, ack stream message
- On final failure (after 3 attempts): update KV status → `failed` with error, ack stream message

### Retry flow

`POST /shares/retry` lists KV keys with prefix `<userId>/`, filters for `status: "failed"`, resets each to `status: "queued"`, and republishes to `shares.process`. Simple and race-free — KV is the source of truth.

## Proposed Changes

### 1. Backend: NATS integration

**`backend/nats.go`** — NATS connection + JetStream + KV setup

- Connect to Scaleway managed NATS (creds via env vars)
- Ensure stream `SHARES` exists with subject `shares.process`
- Ensure KV bucket `SHARE_JOBS` exists (TTL 30d, history 1)
- Expose `ShareQueue` interface for publish, KV read/write, and job listing
- Add to `deps` interface: `GetShareQueue() ShareQueue`

```go
type ShareJob struct {
    UserID    string     `json:"userId"`
    FileID    string     `json:"fileId"`
    FileName  string     `json:"fileName"`
    MimeType  string     `json:"mimeType"`
    Status    string     `json:"status"` // queued, processing, done, failed
    CreatedAt time.Time  `json:"createdAt"`
    Error     string     `json:"error,omitempty"`
    FailedAt  *time.Time `json:"failedAt,omitempty"`
}

type ShareQueue interface {
    // Publish dispatches a job to the stream and writes initial KV state
    Publish(ctx context.Context, job ShareJob) error
    // UpdateStatus updates the KV entry for a job
    UpdateStatus(ctx context.Context, userID, fileID, status string, err error) error
    // ListJobs returns all jobs for a user (prefix scan on KV)
    ListJobs(ctx context.Context, userID string) ([]ShareJob, error)
    // GetJob returns a single job by user + file ID
    GetJob(ctx context.Context, userID, fileID string) (*ShareJob, error)
}
```

**`backend/share_upload.go`** — `POST /share-upload`

- Accept any file type via multipart/form-data (25MB limit)
- Save to user's Drive `shares/` folder
- Create `ShareJob` (status: `queued`) → `ShareQueue.Publish` (writes KV + publishes to stream)
- Deduplication: uses `fileId` as `Nats-Msg-Id` header — retried uploads don't create duplicate jobs
- Return `{ fileId, fileName, status: "queued" }`

**`backend/share_process.go`** — `POST /shares/process` (NATS trigger target)

- Receives NATS message (or HTTP call from Scaleway NATS trigger)
- Extracts `userId` + `fileId` from message body
- Reads full job metadata from KV via `ShareQueue.GetJob`
- Updates KV status → `processing`
- Gets user's Google OAuth token from Clerk via `getGoogleOAuthToken(ctx, msg.UserID)`
- Dispatches by MIME type:
  - `audio/*` → `whisperTranscriber` (existing)
  - `image/*`, `application/pdf` → `gptExampleExtractor` / GPT Vision (existing)
  - Other → no-op, mark done
- On success: save result as companion file in Drive, update KV status → `done`
- On failure: update KV status → `failed` with error info

**`backend/share_list.go`** — `GET /shares`

- Calls `ShareQueue.ListJobs(ctx, userId)` — KV prefix scan on `<userId>/`
- Groups results by status and returns:
```json
{
  "active": [
    { "fileId": "...", "fileName": "photo.jpg", "mimeType": "image/jpeg", "status": "queued", "createdAt": "..." }
  ],
  "failed": [
    { "fileId": "...", "fileName": "recording.m4a", "status": "failed", "error": "Whisper timeout", "failedAt": "..." }
  ],
  "done": [
    { "fileId": "...", "fileName": "notes.pdf", "status": "done", "createdAt": "..." }
  ]
}
```

No per-user subjects needed — KV prefix listing handles user filtering natively.

**`backend/share_retry.go`** — `POST /shares/retry`

- Calls `ShareQueue.ListJobs(ctx, userId)`, filters for `status: "failed"`
- For each failed job: reset KV status → `queued`, republish to `shares.process`
- KV is source of truth — no race conditions with concurrent processing
- Return `{ retriedCount: N }`

**`backend/handler.go`** — add routes

| Method | Path | Auth | Handler | Description |
|--------|------|------|---------|-------------|
| POST | `/share-upload` | Yes | `handleShareUpload` | Upload file + enqueue |
| POST | `/shares/process` | Internal | `handleShareProcess` | NATS trigger target |
| GET | `/shares` | Yes | `handleShareList` | List active + failed jobs |
| POST | `/shares/retry` | Yes | `handleShareRetry` | Retry all failed jobs |

**`backend/clerk_metadata.go`** — add `SharesID` field to `gradeBeeMetadata`

**`backend/setup.go`** — create `shares/` subfolder during setup

### 2. Environment variables (new)

| Variable | Required | Purpose |
|----------|----------|---------|
| `NATS_URL` | Yes | Scaleway managed NATS endpoint |
| `NATS_CREDS` | Yes | NATS credentials (or path to creds file) |
| `PROCESS_SECRET` | Yes | Shared secret for internal `/shares/process` calls |

### 3. Mobile app: `mobile/`

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
│   │   ├── shares.ts         # GET /shares
│   │   └── retry.ts          # POST /shares/retry
│   └── components/
│       ├── JobList.tsx       # Renders active/failed job lists
│       └── StatusBadge.tsx   # "queued" / "processing" / "failed" badge
```

**QueueScreen.tsx:**
- Polls `GET /shares` on focus (or pull-to-refresh)
- Shows two sections: "Processing" (active jobs) and "Failed" (with error messages)
- "Retry All" button calls `POST /shares/retry`, then refreshes
- Each failed job shows filename, error, and timestamp

**Share target registration:**
- **Android:** Expo config plugin adds `<intent-filter>` for `ACTION_SEND` with `*/*` MIME type
- **iOS:** Expo config plugin adds Share Extension via `expo-share-intent`

**Auth flow:**
- Clerk Expo SDK with `expo-secure-store` as token cache
- User signs in once; session persists
- On share intent, if no session → show login, then upload

### 4. Expo build & distribution

- EAS Build for iOS/Android binaries
- TestFlight (iOS) + internal track (Android) for testing

## Drive Folder Structure (updated)

```
GradeBee/
├── uploads/              ← audio files (existing)
├── shares/               ← NEW: shared files from mobile
│   ├── photo.jpg
│   ├── photo.jpg.extracted.txt
│   ├── recording.m4a
│   └── recording.m4a.transcript.txt
├── notes/
├── reports/
├── report-examples/
└── ClassSetup
```

## ✅ Validated: Clerk OAuth token works offline

Confirmed that `getGoogleOAuthToken(ctx, userID)` returns a valid, working Google Drive token when called server-side without an active user session. Clerk auto-refreshes tokens on demand. **No changes needed** — background NATS processing can call `getGoogleOAuthToken` for any user at any time.

## Open Questions

1. **What should processing produce?** Just extracted text stored as companion files? Or auto-feed into the notes/extract pipeline?
2. **What file types to accept?** Start with `*/*` or restrict?
3. **Should the app support manual file picking** in addition to share intent?
4. **File naming in Drive?** Keep original name, prefix with ISO date?
5. **Scaleway NATS trigger format** — need to verify exact payload format for serverless function triggers. May need the process endpoint to accept both HTTP JSON and NATS message formats. **Validate before implementing `share_process.go`.**
6. **HARD BLOCKER: Clerk OAuth token offline access** — verify `getGoogleOAuthToken(ctx, userID)` works without an active user session before starting any implementation. If it doesn't, async processing needs a fundamentally different approach (store refresh tokens ourselves, or process synchronously during upload).

## Effort Estimate

| Task | Estimate |
|------|----------|
| ~~BLOCKER: verify Clerk OAuth token offline access~~ | ~~1 hour~~ ✅ Done |
| Backend: NATS connection + stream + KV setup (`nats.go`) | 2-3 hours |
| Backend: `POST /share-upload` (upload + publish + KV write) | 2-3 hours |
| Backend: `POST /shares/process` (consumer + processor + KV updates) | 3-4 hours |
| Backend: `GET /shares` (KV prefix list) | 1-2 hours |
| Backend: `POST /shares/retry` (KV scan + republish) | 1-2 hours |
| Backend: setup.go + metadata changes | 1 hour |
| Mobile: Expo project setup + Clerk auth | 2-3 hours |
| Mobile: Share intent handling + upload UI | 3-4 hours |
| Mobile: iOS Share Extension config | 2-3 hours |
| Mobile: QueueScreen (active + failed + retry) | 3-4 hours |
| Testing on devices | 2-3 hours |
| EAS Build setup + TestFlight/Play Store | 2-3 hours |
| **Total** | **~4 days** |
