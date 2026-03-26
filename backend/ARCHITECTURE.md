# Backend Architecture

## Overview

Go HTTP backend for GradeBee, a teacher tool for managing student rosters, processing audio recordings (upload → transcribe), and generating report cards. Runs as a standalone HTTP server. Deployed via Docker Compose on a VPS with Caddy for HTTPS and static file serving.

**Package:** `handler` (all source files in `backend/` share this package).

## Entrypoint & Routing

**`handler.go`** — exports `Handle(w, r)`, the single HTTP handler.

| Method | Path         | Auth | Handler              | Description                              |
|--------|-------------|------|----------------------|------------------------------------------|
| GET    | `/` `/health` | No  | inline               | Health check                             |
| POST   | `/setup`    | Yes  | `handleSetup`        | Provision Drive workspace                |
| GET    | `/students` | Yes  | `handleGetStudents`  | Read roster from Sheets                  |
| POST   | `/upload`   | Yes  | `handleUpload`       | Upload audio to Drive + dispatch async job |
| POST   | `/transcribe`| Yes | `handleTranscribe`   | Download from Drive → Whisper → text     |
| POST   | `/extract`  | Yes  | `handleExtract`      | Analyze transcript → matched students    |
| POST   | `/notes`    | Yes  | `handleCreateNotes`  | Create Google Doc notes for students     |
| GET    | `/report-examples` | Yes | `handleListReportExamples` | List example report cards        |
| POST   | `/report-examples` | Yes | `handleUploadReportExample` | Upload example report card      |
| DELETE | `/report-examples` | Yes | `handleDeleteReportExample` | Delete example report card      |
| POST   | `/reports`  | Yes  | `handleGenerateReports` | Generate report cards for students    |
| POST   | `/reports/regenerate` | Yes | `handleRegenerateReport` | Regenerate a report with feedback |
| GET    | `/google-token` | Yes | `handleGoogleToken`  | Return user's Google OAuth access token  |
| POST   | `/drive-import` | Yes | `handleDriveImport`  | Validate + copy Drive file to uploads + dispatch async job |
| GET    | `/jobs`     | Yes  | `handleJobList`      | List user's async upload jobs            |
| POST   | `/jobs/retry` | Yes | `handleJobRetry`    | Retry all failed jobs                    |

Auth is Clerk JWT via `clerkhttp.RequireHeaderAuthorization()` middleware. CORS handled inline.

## Async Upload Processing Pipeline

Audio uploads are processed asynchronously via an in-memory queue (`memQueue`) with a background worker pool. Jobs are dispatched from `POST /upload` and `POST /drive-import` after the file is saved to Drive.

### Flow

```
User uploads audio
        │
        ▼
  POST /upload (or /drive-import)
        │  Saves file to Drive, publishes UploadJob
        │  to memQueue with status "queued"
        │
        ▼
  memQueue worker goroutine
        │  Picks job from buffered channel
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

On failure at any step, the job status is set to `"failed"` with the error message. Users can retry failed jobs via `POST /jobs/retry`, which resets them to `"queued"` and republishes to the queue.

Job status is tracked in-memory (map keyed by `userId/fileId`). The frontend polls `GET /jobs` to show progress.

### Startup

`cmd/server/main.go` calls `InitUploadQueue(ServiceDeps(), 4)` at startup to create the queue with 4 worker goroutines. The queue is shut down gracefully on SIGINT/SIGTERM.

## Dependency Injection

**`deps.go`** — defines `deps` interface + `prodDeps` implementation + package-level `serviceDeps` variable.

```
deps interface {
    GoogleServices(r) → *googleServices
    GoogleServicesForUser(ctx, userID) → *googleServices
    GetTranscriber()  → Transcriber
    GetRoster(ctx, svc) → Roster
    GetDriveStore(svc)  → DriveStore
    GetExtractor()      → Extractor
    GetNoteCreator(svc) → NoteCreator
    GetMetadataIndex(svc) → MetadataIndex
    GetExampleStore(svc)  → ExampleStore
    GetExampleExtractor() → ExampleExtractor
    GetReportGenerator(svc) → ReportGenerator
    GetUploadQueue()        → UploadQueue
    GetGradeBeeMetadata(ctx, userID) → *gradeBeeMetadata
}
```

Tests override `serviceDeps` with stubs. All handler functions call through this interface, never instantiate services directly.

### Key Interfaces

| Interface     | File             | Prod Implementation     | Purpose                        |
|--------------|------------------|------------------------|--------------------------------|
| `deps`       | `deps.go`        | `prodDeps`             | Top-level DI container         |
| `Roster`     | `roster.go`      | `sheetsRoster`         | Read student data from Sheets  |
| `Transcriber`| `transcriber.go` | `whisperTranscriber`   | Audio→text via OpenAI Whisper  |
| `DriveStore` | `drive_store.go` | `sheetsDriveStore`     | Upload/download/copy files on Drive |
| `Extractor`  | `extract.go`     | `gptExtractor`         | Transcript→student extraction  |
| `NoteCreator`| `notes.go`       | `driveNoteCreator`     | Create Google Doc notes        |
| `MetadataIndex` | `metadata_index.go` | `driveMetadataIndex` | Per-student note index (index.json) |
| `ExampleStore` | `report_examples.go` | `driveExampleStore` | CRUD for example report cards  |
| `ExampleExtractor` | `report_example_extractor.go` | `gptExampleExtractor` | GPT Vision text extraction from PDF/images |
| `ReportGenerator` | `report_generator.go` | `gptReportGenerator` | GPT-based report card generation |
| `UploadQueue` | `upload_queue.go` | `memQueue` | In-memory async job queue with worker pool |

## External Services

### Google APIs (`google.go`)
- **Auth flow:** Clerk JWT → extract user ID → `getGoogleOAuthToken` (Clerk Backend API) → OAuth2 token → Drive/Sheets/Docs clients.
- Scope: `drive.file` only (app can only access files it created).
- `googleServices` struct holds `*drive.Service`, `*sheets.Service`, `*docs.Service`, `*clerkUser`.

### Clerk (`auth.go`, `clerk_metadata.go`)
- JWT verification via middleware.
- OAuth token retrieval: `user.ListOAuthAccessTokens` for `oauth_google`.
- **Private metadata** stores Drive resource IDs (`gradeBeeMetadata` struct: folder, spreadsheet, uploads/notes/reports/report-examples subfolder IDs). This avoids needing `drive.readonly` scope to find resources.

### OpenAI Whisper (`transcriber.go`)
- `whisperTranscriber` uses `go-openai` client.
- Handles audio format detection and 3GP→MP4 patching (`audio_format.go`).

## File-by-File Reference

| File                | Responsibility                                                    |
|---------------------|------------------------------------------------------------------|
| `cmd/server/main.go`| Server entrypoint; loads `.env`, inits Clerk, starts queue + HTTP |
| `handler.go`        | Routing, CORS, request logging, `Handle` entrypoint              |
| `deps.go`           | DI interface, prod implementations, `serviceDeps` variable        |
| `google.go`         | Google API client construction, `apiError` type, `createFolder`   |
| `auth.go`           | `getGoogleOAuthToken` — Clerk → Google OAuth token               |
| `clerk_metadata.go` | Read/write `gradeBeeMetadata` in Clerk user private metadata      |
| `setup.go`          | POST /setup — create Drive folder tree + ClassSetup spreadsheet   |
| `students.go`       | GET /students — read & parse roster, `parseStudentRows`           |
| `roster.go`         | `Roster` interface + `sheetsRoster` — Sheets-backed roster reads  |
| `upload.go`         | POST /upload — multipart audio → Drive uploads folder + dispatch job |
| `transcribe.go`     | POST /transcribe — Drive download → Whisper API                   |
| `transcriber.go`    | `Transcriber` interface + `whisperTranscriber` (OpenAI Whisper)   |
| `drive_store.go`    | `DriveStore` interface + `sheetsDriveStore` (Drive CRUD + Copy)   |
| `drive_import.go`   | POST /drive-import — validate + copy Drive file to uploads folder + dispatch job |
| `google_token.go`   | GET /google-token — return user's Google OAuth access token       |
| `extract.go`        | `Extractor` interface + GPT implementation for transcript analysis|
| `extract_handler.go`| POST /extract — transcript analysis → matched students            |
| `notes.go`          | `NoteCreator` interface + Drive/Docs implementation               |
| `notes_handler.go`  | POST /notes — create Google Doc notes for confirmed students      |
| `metadata_index.go` | `MetadataIndex` interface + Drive impl, shared folder utils       |
| `report_examples.go`| `ExampleStore` interface + Drive impl for example report cards    |
| `report_examples_handler.go` | GET/POST/DELETE /report-examples handlers            |
| `report_example_extractor.go` | GPT Vision extraction of text from PDF/image uploads |
| `report_generator.go` | `ReportGenerator` interface + GPT impl, feedback reader        |
| `report_prompt.go`  | GPT prompt construction for report generation                     |
| `reports_handler.go`| POST /reports + POST /reports/regenerate handlers                 |
| `audio_format.go`   | Magic-byte detection, 3GP patching, filename extension fixing     |
| `logger.go`         | slog-based structured logging, request-scoped via context         |
| `upload_queue.go`   | `UploadQueue` interface, `UploadJob` type, job status constants   |
| `mem_queue.go`      | In-memory `UploadQueue` implementation with worker pool           |
| `upload_process.go` | `processUploadJob` pipeline (transcribe→extract→notes)            |
| `jobs_list.go`      | GET /jobs — list user's async upload jobs grouped by status       |
| `jobs_retry.go`     | POST /jobs/retry — reset failed jobs to queued and republish      |

## Drive Folder Structure (per user)

```
GradeBee/              ← root folder (ID in metadata.FolderID)
├── uploads/           ← audio files (metadata.UploadsID)
├── notes/             ← (metadata.NotesID)
│   └── {class}/
│       └── {student}/
│           ├── index.json       ← note metadata index
│           └── {student — date} ← Google Doc notes
├── reports/           ← (metadata.ReportsID)
│   └── {YYYY-MM}/
│       └── {student — class}    ← Google Doc report cards
├── report-examples/   ← (metadata.ReportExamplesID) plain text example report cards
└── ClassSetup         ← Google Sheet (metadata.SpreadsheetID)
    └── Sheet "Students": columns A=class, B=student (header row 1)
```

## Error Handling

`apiError` struct (`google.go`) carries HTTP status, machine-readable code, and human message. Handlers check `errors.As(err, &apiError)` and call `writeAPIError`. All responses are JSON.

## Testing

- Tests in `*_test.go` files override `serviceDeps` with stubs.
- `testutil_test.go` has shared test helpers (`stubUploadQueue`, `mockDepsAll`, etc.).
- Run: `make test` / `make lint`

## Environment Variables

| Variable           | Required | Purpose                          |
|-------------------|----------|----------------------------------|
| `CLERK_SECRET_KEY`| Yes      | Clerk Backend API key            |
| `OPENAI_API_KEY`  | Yes      | Whisper transcription            |
| `ALLOWED_ORIGIN`  | No       | CORS origin (default `*`)       |
| `PORT`            | No       | Local dev port (default `8080`) |
| `LOG_LEVEL`       | No       | DEBUG/INFO/WARN/ERROR/off        |
| `LOG_FORMAT`      | No       | `json` for JSON, else text       |
