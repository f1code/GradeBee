# Phase 1: NATS Infrastructure + Processing Pipeline

## Goal

Stand up NATS JetStream infrastructure (stream + KV), the async processing pipeline, and job management endpoints. Existing web flow (`/transcribe`, `/extract`, `/notes`) remains untouched — Phase 1 runs alongside it. No frontend changes.

**Milestone:** Can manually publish a job to NATS and see notes auto-created in Drive.

## Proposed Changes

### 1. `backend/nats.go` — NATS connection, stream, KV, `UploadQueue`

**New file.** Owns all NATS/JetStream interaction.

#### Types

```go
type UploadJob struct {
    UserID    string     `json:"userId"`
    FileID    string     `json:"fileId"`
    FileName  string     `json:"fileName"`
    MimeType  string     `json:"mimeType"`
    Source    string     `json:"source"` // "web" or "mobile"
    Status    string     `json:"status"` // queued|transcribing|extracting|creating_notes|done|failed
    CreatedAt time.Time  `json:"createdAt"`
    NoteIDs   []string   `json:"noteIds,omitempty"`
    Error     string     `json:"error,omitempty"`
    FailedAt  *time.Time `json:"failedAt,omitempty"`
}

type UploadQueue interface {
    Publish(ctx context.Context, job UploadJob) error
    GetJob(ctx context.Context, userID, fileID string) (*UploadJob, error)
    UpdateJob(ctx context.Context, job UploadJob) error
    ListJobs(ctx context.Context, userID string) ([]UploadJob, error)
    Close()
}
```

#### `NewUploadQueue(natsURL, natsCreds string) (UploadQueue, error)`

- Connect via `nats.Connect(natsURL, nats.UserCredentials(natsCreds))` (or inline creds if `NATS_CREDS` is not a file path — detect with `os.Stat`)
- Get JetStream context
- `js.AddStream` — name `UPLOADS`, subject `uploads.process`, max age 7d, duplicate window 5min. Use `nats.StreamConfig` update-if-exists pattern.
- `js.CreateKeyValue` — bucket `UPLOAD_JOBS`, TTL 30d, history 1
- Return `&natsUploadQueue{js, kv}`

#### `natsUploadQueue` methods

**`Publish`**: Marshal `job` to JSON, `kv.Put("<userId>/<fileId>", data)` with status `queued`, then `js.Publish("uploads.process", payload, nats.MsgId(job.FileID))`. KV write first so status is visible immediately; dedup header prevents double-processing.

**`GetJob`**: `kv.Get("<userId>/<fileId>")` → unmarshal.

**`UpdateJob`**: `kv.Put("<userId>/<fileId>", marshal(job))`. Caller sets all fields (read-modify-write pattern).

**`ListJobs`**: `kv.Keys()` filtered by prefix `<userId>/` → `kv.Get` each. (NATS KV doesn't support prefix scan natively, but `Keys()` + client-side filter works fine at our scale — tens of jobs per user, not thousands.)

**`Close`**: `nc.Close()`

#### Integration with `deps`

Add to `deps` interface:
```go
GetUploadQueue() (UploadQueue, error)
```

`prodDeps` implementation: lazy-init singleton (package-level `sync.Once` + `var uploadQueueInstance`). Reads `NATS_URL` and `NATS_CREDS` from env. Returns error if env vars missing.

---

### 2. `backend/upload_process.go` — `POST /jobs/process`

**New file.** The NATS consumer / processing pipeline handler.

#### `handleJobProcess(w http.ResponseWriter, r *http.Request)`

**Auth:** Not Clerk JWT — internal-only. Validate `Authorization: Bearer <PROCESS_SECRET>` header against `os.Getenv("PROCESS_SECRET")`. Return 401 if mismatch.

**Request body:**
```json
{ "userId": "user_xxx", "fileId": "drive_file_id" }
```

**Pipeline logic (extracted into `processUploadJob` for testability):**

```go
func processUploadJob(ctx context.Context, queue UploadQueue, userID, fileID string) error
```

Steps:

1. `queue.GetJob(ctx, userID, fileID)` → get full job metadata. If not found or status != `queued`, log and return nil (idempotent).

2. **Build Google services without `*http.Request`:** Need a new helper:
   ```go
   func newGoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error)
   ```
   Add to `google.go`. Same as `newGoogleServices` but takes userID directly instead of extracting from Clerk session claims. Calls `getGoogleOAuthToken(ctx, userID)` → builds Drive/Sheets/Docs clients.

3. **Update status → `transcribing`**, then:
   - `store.Download(ctx, fileID)` to get audio
   - `store.FileName(ctx, fileID)` for extension
   - Build Whisper prompt from roster class names (best-effort, log warning on failure)
   - `transcriber.Transcribe(ctx, fileName, body, prompt)`

4. **Update status → `extracting`**, then:
   - `roster.Students(ctx)` for class groups
   - `extractor.Extract(ctx, ExtractRequest{Transcript: transcript, Classes: classes})`

5. **Update status → `creating_notes`**, then:
   - Get `gradeBeeMetadata` via `getGradeBeeMetadata(ctx, userID)`
   - For each `MatchedStudent` with confidence ≥ threshold (reuse existing logic from `extract_handler.go` if any, otherwise ≥ 0.5):
     - `noteCreator.CreateNote(ctx, CreateNoteRequest{...})`
   - Collect `noteIds`

6. **Update status → `done`** with `noteIds`.

**Error handling:**
- Any step failure → `updateJob` with status `failed`, error message, `failedAt` timestamp
- Return the error (HTTP handler writes 500)
- NATS redelivery (max 3) will re-enter; step 1's status check means a `failed` job won't be reprocessed (must go through `/jobs/retry` to reset to `queued`)
- Actually — on transient failure we want NATS to retry. So: on failure, update KV to `failed` **only if this is the last delivery attempt**. Problem: HTTP trigger doesn't carry delivery count. **Decision:** Always set `failed` on error. The retry endpoint is the mechanism for re-attempts. This is simpler than tracking delivery count.

**Handler wiring:** In `handler.go`, add route for `POST /jobs/process` — no Clerk middleware, just the shared secret check.

---

### 3. `backend/google.go` — New helper

Add `newGoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error)`:

```go
func newGoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error) {
    accessToken, err := getGoogleOAuthToken(ctx, userID)
    if err != nil {
        return nil, fmt.Errorf("google services for user %s: %w", userID, err)
    }
    tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
    driveSrv, _ := drive.NewService(ctx, option.WithTokenSource(tokenSource))
    sheetsSrv, _ := sheets.NewService(ctx, option.WithTokenSource(tokenSource))
    docsSrv, _ := docs.NewService(ctx, option.WithTokenSource(tokenSource))
    return &googleServices{Drive: driveSrv, Sheets: sheetsSrv, Docs: docsSrv, User: &clerkUser{UserID: userID}}, nil
}
```

(With proper error handling on each `NewService` call, matching existing pattern.)

Also add to `deps` interface:
```go
GoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error)
```

`prodDeps` implementation delegates to `newGoogleServicesForUser`. Tests can stub it.

---

### 4. `backend/jobs_list.go` — `GET /jobs`

**New file.**

#### `handleJobList(w http.ResponseWriter, r *http.Request)`

- Clerk auth (standard middleware)
- Extract userID from session claims
- `queue.ListJobs(ctx, userID)`
- Group into response buckets:

```go
type jobListResponse struct {
    Active []UploadJob `json:"active"` // queued, transcribing, extracting, creating_notes
    Failed []UploadJob `json:"failed"`
    Done   []UploadJob `json:"done"`
}
```

- Sort each bucket by `CreatedAt` descending
- `writeJSON(w, 200, response)`

---

### 5. `backend/jobs_retry.go` — `POST /jobs/retry`

**New file.**

#### `handleJobRetry(w http.ResponseWriter, r *http.Request)`

- Clerk auth
- Extract userID from session claims
- `queue.ListJobs(ctx, userID)` → filter `status == "failed"`
- For each failed job:
  - Reset: `status = "queued"`, clear `error`, clear `failedAt`
  - `queue.Publish(ctx, job)` (this does KV put + stream publish)
- Return `{ "retriedCount": N }`

---

### 6. `backend/handler.go` — Route additions

Add handler vars:
```go
jobListHandler  = clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(handleJobList))
jobRetryHandler = clerkhttp.RequireHeaderAuthorization()(http.HandlerFunc(handleJobRetry))
```

Add routes in `Handle` switch:
```go
case "GET" with path "/jobs":     jobListHandler.ServeHTTP(w, r)
case "POST" with path "/jobs/retry":  jobRetryHandler.ServeHTTP(w, r)
case "POST" with path "/jobs/process": handleJobProcess(w, r)  // no Clerk middleware
```

No existing routes removed.

---

### 7. `backend/deps.go` — Interface additions

Add two methods to `deps`:
```go
GetUploadQueue() (UploadQueue, error)
GoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error)
```

`prodDeps` implementations:
- `GetUploadQueue`: singleton via `sync.Once`, reads `NATS_URL`/`NATS_CREDS` env vars
- `GoogleServicesForUser`: delegates to `newGoogleServicesForUser`

---

### 8. Environment variables (new)

| Variable | Required | Purpose |
|----------|----------|---------|
| `NATS_URL` | Yes (for NATS features) | Scaleway managed NATS endpoint |
| `NATS_CREDS` | Yes (for NATS features) | NATS credentials (file path or inline) |
| `PROCESS_SECRET` | Yes | Shared secret for `/jobs/process` auth |

If `NATS_URL` is not set, `GetUploadQueue` returns an error. Existing endpoints remain unaffected (they don't call `GetUploadQueue` in Phase 1).

---

### 9. Go dependencies (new)

Add to `go.mod`:
```
github.com/nats-io/nats.go  (latest, includes JetStream + KV)
```

---

## Test Strategy

### `backend/nats_test.go` — Unit tests for `UploadQueue`

Use `github.com/nats-io/nats-server/v2/server` embedded test server (in-process, no external deps).

Tests:
- **`TestPublishAndGet`**: Publish a job, verify `GetJob` returns it with status `queued`
- **`TestUpdateJob`**: Publish, update status to `transcribing`, verify `GetJob` reflects change
- **`TestListJobs`**: Publish 3 jobs for userA, 1 for userB. `ListJobs(userA)` returns 3.
- **`TestPublishDedup`**: Publish same fileID twice, verify only one stream message (read from consumer)
- **`TestKVTTL`**: (may skip — TTL is config, hard to test without waiting) Document that TTL is set to 30d in config.

### `backend/upload_process_test.go` — Pipeline tests

Stub `deps` with:
- `stubUploadQueue` (in-memory map implementing `UploadQueue`)
- Existing test stubs for `Transcriber`, `Extractor`, `NoteCreator`, `DriveStore`, `Roster`
- `stubGoogleServicesForUser` returning stubbed services

Tests:
- **`TestProcessJob_HappyPath`**: Queue a job, call `processUploadJob`, verify KV status transitions (transcribing → extracting → creating_notes → done), verify `noteIds` populated
- **`TestProcessJob_TranscribeFail`**: Stub transcriber to error. Verify status = `failed`, error message set.
- **`TestProcessJob_ExtractFail`**: Similar for extractor failure.
- **`TestProcessJob_NoteCreateFail`**: Partial note creation failure. Verify status = `failed`.
- **`TestProcessJob_AlreadyProcessed`**: Job with status `done` — verify no-op.
- **`TestProcessJob_MissingMetadata`**: User has no `gradeBeeMetadata`. Verify status = `failed` with clear error.

### `backend/jobs_list_test.go`

- **`TestJobList_GroupsByStatus`**: Seed jobs in various statuses, verify JSON response groups correctly.
- **`TestJobList_EmptyUser`**: No jobs → empty arrays (not null).

### `backend/jobs_retry_test.go`

- **`TestJobRetry_RetriesFailedOnly`**: Seed mix of done/failed/queued jobs. Verify only failed ones are reset and republished. Verify `retriedCount`.
- **`TestJobRetry_NoFailedJobs`**: Returns `{ "retriedCount": 0 }`.

### `backend/upload_process_handler_test.go`

- **`TestJobProcessAuth_ValidSecret`**: Correct `PROCESS_SECRET` → 200.
- **`TestJobProcessAuth_InvalidSecret`**: Wrong secret → 401.
- **`TestJobProcessAuth_MissingHeader`**: No auth header → 401.

### `backend/integration_test.go` — End-to-end NATS integration test

Uses embedded NATS server + stubbed external services (Google, Whisper, GPT). Tests the full flow: publish → consumer picks up → pipeline runs → KV updated → job list reflects result.

**`TestIntegration_PublishToNoteCreation`**:
1. Start embedded NATS server, create real `natsUploadQueue`
2. Swap `serviceDeps` with stubs for `Transcriber` (returns canned transcript), `Extractor` (returns 2 matched students), `NoteCreator` (records calls, returns fake doc IDs), `DriveStore` (returns canned audio bytes), `Roster` (returns canned class list), `GoogleServicesForUser` (returns stub google services)
3. Publish a job via `queue.Publish(ctx, job)` with status `queued`
4. Call `processUploadJob(ctx, ...)` (simulating what the worker does)
5. Assert:
   - `queue.GetJob` returns status `done` with 2 `noteIds`
   - `NoteCreator.CreateNote` was called twice with correct student names
   - `Transcriber.Transcribe` was called once with correct fileID
   - `Extractor.Extract` was called once with the canned transcript

**`TestIntegration_PublishToFailure`**:
1. Same setup but stub `Transcriber` to return error
2. Publish + process
3. Assert: job status `failed`, error contains transcriber error message

**`TestIntegration_RetryAfterFailure`**:
1. Publish job, process with failing transcriber → status `failed`
2. Fix transcriber stub to succeed
3. Call `handleJobRetry` (via HTTP test) — verifies job reset to `queued` + republished
4. Process again → status `done`
5. Assert: full round-trip from failure through retry to success

**`TestIntegration_ListJobsDuringProcessing`**:
1. Publish 3 jobs: process first to `done`, second to `failed`, leave third as `queued`
2. Call `handleJobList` via HTTP test
3. Assert response has correct grouping: 1 active (queued), 1 failed, 1 done

These tests use the **real `natsUploadQueue`** against an embedded server — they verify that KV reads/writes, stream publishing, and the pipeline all work together correctly. This is the gap between the unit tests (which test NATS or pipeline in isolation) and manual testing.

### Stub `UploadQueue` for handler tests

```go
type stubUploadQueue struct {
    jobs map[string]UploadJob // key: "userId/fileId"
    published []UploadJob
}
```

Implements `UploadQueue` interface with in-memory storage. Used by all non-NATS tests.

---

## File Summary

| File | Action | Description |
|------|--------|-------------|
| `backend/nats.go` | New | NATS connection, stream/KV setup, `UploadQueue` interface + `natsUploadQueue` impl |
| `backend/upload_process.go` | New | `handleJobProcess` + `processUploadJob` pipeline + exported `ProcessUploadJob` |
| `backend/jobs_list.go` | New | `handleJobList` — GET /jobs |
| `backend/jobs_retry.go` | New | `handleJobRetry` — POST /jobs/retry |
| `backend/google.go` | Edit | Add `newGoogleServicesForUser(ctx, userID)` |
| `backend/deps.go` | Edit | Add `GetUploadQueue()` and `GoogleServicesForUser()` to `deps` interface + `prodDeps` |
| `backend/handler.go` | Edit | Add routes for `/jobs`, `/jobs/retry`, `/jobs/process` |
| `backend/Makefile` | Edit | Add `nats`, `worker`, `dev` targets |
| `.env.example` | Edit | Add `NATS_URL`, `NATS_CREDS`, `PROCESS_SECRET` |
| `backend/cmd/worker/main.go` | New | Local NATS consumer — pulls from stream, calls `ProcessUploadJob` |
| `docker-compose.yml` | New | Local NATS server with JetStream |
| `backend/nats_test.go` | New | UploadQueue unit tests with embedded NATS server |
| `backend/upload_process_test.go` | New | Pipeline integration tests with stubs |
| `backend/jobs_list_test.go` | New | Job list handler tests |
| `backend/jobs_retry_test.go` | New | Job retry handler tests |
| `backend/integration_test.go` | New | End-to-end test: real NATS (embedded) + stubbed services, full publish→process→list→retry flow |

## Local Development

### Local NATS server

Production uses Scaleway managed NATS. For local dev, run NATS in Docker with JetStream enabled:

**`docker-compose.yml`** (project root, new file):
```yaml
services:
  nats:
    image: nats:latest
    command: ["--jetstream", "--store_dir", "/data"]
    ports:
      - "4222:4222"   # client
      - "8222:8222"   # monitoring
    volumes:
      - nats-data:/data

volumes:
  nats-data:
```

Local NATS needs no credentials — connect with `nats://localhost:4222`. The `nats.go` connection logic should handle this: if `NATS_CREDS` is empty, connect without credentials.

**`.env.example`** additions:
```
# NATS (local: docker compose up nats)
NATS_URL=nats://localhost:4222
NATS_CREDS=
PROCESS_SECRET=dev-secret
```

### Local consumer: `cmd/worker/main.go`

In production, Scaleway triggers `/jobs/process` via HTTP when a NATS message arrives. Locally there's no such trigger, so we need a small worker binary that subscribes to the stream and calls the processing pipeline directly.

**New file: `backend/cmd/worker/main.go`**

```go
func main() {
    // Load .env, init Clerk SDK (same as cmd/server/main.go)
    // Connect to NATS, create pull consumer on UPLOADS stream
    // Loop: fetch message → call handler.ProcessUploadJob(ctx, msg.UserID, msg.FileID)
    // Ack on success, log on failure
}
```

Key details:
- Uses `js.PullSubscribe("uploads.process", "upload-processor")` with `nats.AckWait(5 * time.Minute)`
- Calls `processUploadJob` directly (not HTTP) — this function must be **exported** as `ProcessUploadJob` for the worker to use
- On error: nack with delay (let NATS redeliver). After max deliveries, ack + mark failed in KV.
- Graceful shutdown on SIGINT/SIGTERM: drain subscription, wait for in-flight job.

Export from handler package:
```go
// ProcessUploadJob is the exported entry point for the worker binary.
func ProcessUploadJob(ctx context.Context, userID, fileID string) error {
    return processUploadJob(ctx, serviceDeps, userID, fileID)
}
```

### Makefile additions

```makefile
.PHONY: dev worker nats

nats:
	docker compose up nats -d

worker: nats
	cd cmd/worker && go run .

dev: nats
	cd cmd/server && go run .
```

### Local testing workflow

1. `docker compose up nats -d` (or `make nats`)
2. Terminal 1: `make dev` — starts HTTP server on :8080
3. Terminal 2: `make worker` — starts NATS consumer
4. Upload via web UI or curl → job published to NATS → worker picks it up → notes created in Drive

### Manual testing with NATS CLI

Install `nats` CLI for inspecting streams/KV:

```bash
brew install nats-io/nats-tools/nats

# Check stream
nats stream info UPLOADS
nats stream view UPLOADS

# Check KV
nats kv ls UPLOAD_JOBS
nats kv get UPLOAD_JOBS "user_xxx/file_yyy"

# Manually publish a test job
nats pub uploads.process '{"userId":"user_xxx","fileId":"file_yyy"}'
```

### Automated tests

Automated tests (in `*_test.go`) use the embedded NATS server (`github.com/nats-io/nats-server/v2/server`) — no Docker required. `make test` works without any external dependencies, same as today.

---

## Open Questions

1. **NATS KV `Keys()` performance** — `Keys()` returns all keys in the bucket, then we filter client-side by prefix. Fine for tens/hundreds of jobs. If scale becomes an issue, could switch to a subject-based KV bucket per user, but premature now.

2. **Scaleway NATS trigger payload format** — The `/jobs/process` endpoint assumes a simple JSON body `{userId, fileId}`. Need to verify Scaleway's serverless NATS trigger delivers the NATS message payload as the HTTP body, or if it wraps it. **Action:** Test with a hello-world trigger before implementing. If wrapped, add an unwrap layer in `handleJobProcess`.

3. **Confidence threshold for auto-creating notes** — Current web flow lets the user confirm/reject each student match. Auto-create needs a threshold. Suggest **0.5** (matching the existing UI which shows low-confidence as yellow). Could be an env var. Decide before implementing.

4. **Should `processUploadJob` use `deps` or accept interfaces directly?** — Using `deps` is consistent with existing handlers but requires `serviceDeps` to be set. For testability, `processUploadJob` should accept explicit interfaces:
   ```go
   func processUploadJob(ctx context.Context, queue UploadQueue, svc *googleServices, d deps) error
   ```
   This way tests pass stubs directly without touching the global `serviceDeps`.
