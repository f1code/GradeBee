# Backend Architecture

## Overview

Go HTTP backend for GradeBee, a teacher tool for managing student rosters, processing audio recordings (upload → transcribe), and generating report cards. Runs as a standalone HTTP server. Deployed via Docker Compose on a VPS with Caddy for HTTPS and static file serving.

**Package:** `handler` (all source files in `backend/` share this package).

**Storage:** SQLite database (`modernc.org/sqlite`) with WAL mode. Audio files stored on local disk. No Google Sheets/Docs — all data in SQLite.

## Entrypoint & Routing

**`handler.go`** — exports `Handle(w, r)`, the single HTTP handler. Routes use `strings.HasPrefix` + `pathParam()` for parameterized paths.

| Method | Path | Auth | Handler | Description |
|--------|------|------|---------|-------------|
| GET | `/` `/health` | No | inline | Health check |
| GET | `/classes` | Yes | `handleListClasses` | List user's classes with student counts |
| POST | `/classes` | Yes | `handleCreateClass` | Create a class |
| PUT | `/classes/{id}` | Yes | `handleUpdateClass` | Rename a class |
| DELETE | `/classes/{id}` | Yes | `handleDeleteClass` | Delete class + cascade |
| GET | `/classes/{id}/students` | Yes | `handleListStudents` | List students in a class |
| POST | `/classes/{id}/students` | Yes | `handleCreateStudent` | Add a student |
| GET | `/students` | Yes | `handleGetStudents` | Full roster grouped by class |
| PUT | `/students/{id}` | Yes | `handleUpdateStudent` | Rename / move student |
| DELETE | `/students/{id}` | Yes | `handleDeleteStudent` | Delete student + cascade |
| GET | `/students/{id}/notes` | Yes | `handleListNotes` | List notes for a student |
| POST | `/students/{id}/notes` | Yes | `handleCreateNote` | Create a manual note |
| GET | `/notes/{id}` | Yes | `handleGetNote` | Get single note |
| PUT | `/notes/{id}` | Yes | `handleUpdateNote` | Edit note summary |
| DELETE | `/notes/{id}` | Yes | `handleDeleteNote` | Delete a note |
| POST | `/reports` | Yes | `handleGenerateReports` | Generate report cards (returns HTML) |
| POST | `/reports/{id}/regenerate` | Yes | `handleRegenerateReport` | Regenerate with feedback |
| GET | `/students/{id}/reports` | Yes | `handleListReports` | List reports for a student |
| GET | `/reports/{id}` | Yes | `handleGetReport` | Get single report HTML |
| DELETE | `/reports/{id}` | Yes | `handleDeleteReport` | Delete a report |
| GET | `/report-examples` | Yes | `handleListReportExamples` | List example report cards |
| POST | `/report-examples` | Yes | `handleUploadReportExample` | Upload example report card |
| DELETE | `/report-examples` | Yes | `handleDeleteReportExample` | Delete example report card |
| PUT | `/report-examples/{id}` | Yes | `handleUpdateReportExample` | Update example report card |
| POST | `/voice-notes/upload` | Yes | `handleUpload` | Upload audio to disk + dispatch job |
| POST | `/voice-notes/drive-import` | Yes | `handleDriveImport` | Download from Drive + dispatch job |
| GET | `/google-token` | Yes | `handleGoogleToken` | Return Google OAuth token for Drive Picker |
| GET | `/voice-notes/jobs` | Yes | `handleJobList` | List user's async upload jobs |
| POST | `/voice-notes/jobs/retry` | Yes | `handleJobRetry` | Retry failed jobs |
| POST | `/voice-notes/jobs/dismiss` | Yes | `handleJobDismiss` | Dismiss completed/failed jobs |

Auth is Clerk JWT via `clerkhttp.RequireHeaderAuthorization()` middleware. CORS handled inline (GET, POST, PUT, DELETE, OPTIONS).

## Async Upload Processing Pipeline

Audio uploads are processed asynchronously via a generic in-memory queue (`MemQueue[VoiceNoteJob]`) with a background worker pool. Jobs are dispatched from `POST /voice-notes/upload` and `POST /voice-notes/drive-import` after the file is saved to disk.

### Flow

```
User uploads audio
        │
        ▼
  POST /voice-notes/upload (or /voice-notes/drive-import)
        │  Saves file to disk, creates voice_notes row,
        │  publishes VoiceNoteJob to MemQueue
        │
        ▼
  MemQueue worker goroutine
        │  Picks job key from buffered channel
        │
        ▼
  processVoiceNote(ctx, deps, queue, key)
        │
        ├─ Idempotency check: skip if job status ≠ "queued"
        │
        ├─ Step 1: Transcribe (status → "transcribing")
        │    Read audio from local disk → OpenAI Whisper
        │    Whisper prompt seeded with class names from DB roster
        │
        ├─ Step 2: Extract (status → "extracting")
        │    Send transcript + student roster to GPT
        │    → per-student observations (name, class, summary, confidence)
        │
        ├─ Step 3: Create Notes (status → "creating_notes")
        │    For each student with confidence ≥ 0.5:
        │      Resolve name → student ID via FindByNameAndClass
        │      Create note in SQLite via dbNoteCreator
        │
        └─ Done (status → "done", mark voice note processed)
```

On failure at any step, the job status is set to `"failed"` with the error message. Users can retry failed jobs via `POST /voice-notes/jobs/retry`.

Job status is tracked in-memory (map keyed by `userId/<uploadId>`). The frontend polls `GET /voice-notes/jobs` to show progress.

### Startup

`cmd/server/main.go` calls `InitVoiceNoteQueue(d, 4)` at startup to create the queue with 4 worker goroutines. The queue is shut down gracefully on SIGINT/SIGTERM.

### Voice Note Cleanup

`voice_note_cleanup.go` runs a background goroutine that deletes processed audio files from disk and their `voice_notes` rows after a retention period (default 7 days, configurable via `UPLOAD_RETENTION_HOURS`).

### Generic Queue Infrastructure

The queue system uses Go generics for type safety:

- **`Keyed`** — constraint interface requiring `JobKey() string` and `OwnerID() string`
- **`JobQueue[T Keyed]`** — generic interface for async job operations (Publish, GetJob, UpdateJob, ListJobs, DeleteJob, Close)
- **`MemQueue[T Keyed]`** — in-memory implementation with buffered channel + worker pool
- **`ProcessFunc[T Keyed]`** — function type called by workers: `func(ctx, queue, key) error`

Each job type gets its own queue instance. The processor function is injected at construction via closure, keeping the generic queue status-agnostic.

### Report Example Extraction Pipeline

PDF and image report card uploads are processed asynchronously:

```
User uploads PDF/image
        │
        ▼
  POST /report-examples (or /drive-import-example)
        │  Saves file to disk, creates report_examples row
        │  with status='processing', publishes ExtractionJob
        │
        ▼
  MemQueue[ExtractionJob] worker goroutine
        │
        ├─ Read file from disk
        ├─ For PDFs: convert to JPEG images via pdftoppm (150 DPI)
        ├─ Send each page to GPT Vision (parallel, structured JSON output)
        ├─ Update report_examples row: status='ready', content=extracted text
        └─ Clean up temp file from disk
```

Text file uploads (plain text, JSON body) are stored synchronously with `status='ready'`.

The frontend polls `GET /report-examples` every 3s while any example has `status='processing'`.

## Dependency Injection

**`deps.go`** — defines `deps` interface + `prodDeps` implementation + package-level `serviceDeps` variable.

```
deps interface {
    GetTranscriber()      → Transcriber
    GetRoster(ctx, userID) → Roster
    GetExtractor()        → Extractor
    GetNoteCreator()      → NoteCreator
    GetExampleStore()     → ExampleStore
    GetExampleExtractor() → ExampleExtractor
    GetReportGenerator()  → ReportGenerator
    GetVoiceNoteQueue()   → JobQueue[VoiceNoteJob]
    GetDriveClient(ctx, userID) → DriveClient
    GetDB()               → *sql.DB
    GetClassRepo()        → *ClassRepo
    GetStudentRepo()      → *StudentRepo
    GetNoteRepo()         → *NoteRepo
    GetReportRepo()       → *ReportRepo
    GetExampleRepo()      → *ReportExampleRepo
    GetVoiceNoteRepo()    → *VoiceNoteRepo
    GetUploadsDir()       → string
}
```

Tests override `serviceDeps` with stubs. All handler functions call through this interface, never instantiate services directly.

### Key Interfaces

| Interface | File | Prod Implementation | Purpose |
|-----------|------|---------------------|---------|
| `deps` | `deps.go` | `prodDeps` | Top-level DI container |
| `Roster` | `roster.go` | `dbRoster` | Read student data from DB |
| `Transcriber` | `transcriber.go` | `whisperTranscriber` | Audio→text via OpenAI Whisper |
| `Extractor` | `extract.go` | `gptExtractor` | Transcript→student extraction |
| `NoteCreator` | `notes.go` | `dbNoteCreator` | Create notes in SQLite |
| `ExampleStore` | `report_examples.go` | `dbExampleStore` | CRUD for example report cards |
| `ExampleExtractor` | `report_example_extractor.go` | `gptExampleExtractor` | GPT Vision text extraction from images; PDF→image via pdftoppm |
| `ReportGenerator` | `report_generator.go` | `gptReportGenerator` | GPT-based report card generation (HTML output) |
| `JobQueue[VoiceNoteJob]` | `job_queue.go` | `MemQueue[VoiceNoteJob]` | Generic in-memory async job queue with worker pool |
| `JobQueue[ExtractionJob]` | `job_queue.go` | `MemQueue[ExtractionJob]` | Async report example extraction queue |

## External Services

### Google OAuth (`google.go`)
- Auth: Clerk JWT → extract user ID → Google OAuth token (used for Drive Picker import).
- **Note:** Google Drive integration is being removed. Drive import functionality is deprecated.

### Clerk (`auth.go`)
- JWT verification via middleware.
- OAuth token retrieval: `user.ListOAuthAccessTokens` for `oauth_google`.
- `userIDFromRequest(r)` extracts user ID from Clerk session claims.

### OpenAI Whisper (`transcriber.go`)
- `whisperTranscriber` uses `go-openai` client.
- Handles audio format detection and 3GP→MP4 patching (`audio_format.go`).

## Database

SQLite with WAL mode (`db.go`). Migrations embedded via `embed.FS` (`migrate.go`, `sql/001_init.sql`).

### Tables

| Table | Purpose |
|-------|---------|
| `classes` | Teacher's classes (user_id + name) |
| `students` | Students belonging to classes |
| `notes` | Observation notes per student |
| `reports` | Generated HTML report cards |
| `report_examples` | Example report cards for style matching |
| `voice_notes` | Audio file tracking (file path, processed_at) |

### Repository Layer

Each table has a `Repo*` type in `repo_*.go` files providing type-safe CRUD.

## Authorization Pattern

All CRUD endpoints verify resource ownership:
1. Extract `userID` from Clerk JWT claims
2. For class operations: query class, check `class.UserID == userID`
3. For student operations: `studentRepo.BelongsToUser(studentID, userID)`
4. For note/report operations: join through student → class to verify ownership

## File-by-File Reference

| File | Responsibility |
|------|---------------|
| `cmd/server/main.go` | Server entrypoint; loads `.env`, inits Clerk, opens DB, runs migrations, starts queue + cleanup + HTTP |
| `handler.go` | Routing, CORS, request logging, `Handle` entrypoint, `userIDFromRequest`, `pathParam` |
| `deps.go` | DI interface, prod implementations, `serviceDeps` variable |
| `google.go` | `apiError` type, `writeAPIError`, `newDriveReadClient` (Drive-read-only) |
| `auth.go` | `getGoogleOAuthToken` — Clerk → Google OAuth token |
| `db.go` | Open SQLite, set PRAGMAs (WAL, busy_timeout, foreign_keys) |
| `migrate.go` | Embed + run SQL migrations on startup |
| `sql/001_init.sql` | Schema: classes, students, notes, reports, report_examples, uploads (renamed to voice_notes via 002) |
| `sql/002_rename_uploads.sql` | Migration: rename uploads → voice_notes, update indexes |
| `repo_class.go` | `ClassRepo` — CRUD for classes |
| `repo_student.go` | `StudentRepo` — CRUD for students, `FindByNameAndClass`, `BelongsToUser` |
| `repo_note.go` | `NoteRepo` — CRUD for notes, `ListForStudents` (date range) |
| `repo_report.go` | `ReportRepo` — CRUD for reports |
| `repo_example.go` | `ReportExampleRepo` — CRUD for report examples |
| `repo_voice_note.go` | `VoiceNoteRepo` — CRUD for voice_notes, `MarkProcessed`, `ListStale` |
| `repo_errors.go` | `ErrNotFound`, `ErrDuplicate`, `isDuplicateErr` |
| `students.go` | GET /students, class/student CRUD handlers, `classGroup`/`student` types |
| `roster.go` | `Roster` interface + `dbRoster` — DB-backed roster reads |
| `voice_note_upload.go` | POST /voice-notes/upload — multipart audio → disk + voice_notes table + dispatch job |
| `transcriber.go` | `Transcriber` interface + `whisperTranscriber` (OpenAI Whisper) |
| `voice_note_drive_import.go` | POST /voice-notes/drive-import — download from Drive → disk + voice_notes table + dispatch job |
| `google_token.go` | GET /google-token — return user's Google OAuth access token |
| `extract.go` | `Extractor` interface + GPT implementation for transcript analysis |
| `notes.go` | `NoteCreator` interface + `dbNoteCreator`, note CRUD handlers |
| `report_examples.go` | `ExampleStore` interface + `dbExampleStore` |
| `report_examples_handler.go` | GET/POST/DELETE /report-examples handlers |
| `report_example_extractor.go` | GPT Vision extraction of text from image uploads; PDF→JPEG conversion via pdftoppm |
| `report_example_job.go` | `ExtractionJob` type for async report example extraction |
| `report_example_process.go` | `processExtraction` pipeline (read file→extract→update DB) |
| `report_generator.go` | `ReportGenerator` interface + `gptReportGenerator` (HTML output) |
| `report_prompt.go` | GPT prompt construction for report generation (requests HTML output) |
| `reports_handler.go` | POST /reports, POST /reports/{id}/regenerate, report CRUD handlers |
| `audio_format.go` | Magic-byte detection, 3GP patching, filename extension fixing |
| `logger.go` | slog-based structured logging, request-scoped via context |
| `job_queue.go` | `Keyed` constraint, `JobQueue[T]` generic interface for async job queues |
| `job_queue_mem.go` | `MemQueue[T]` — generic in-memory `JobQueue` implementation with worker pool |
| `voice_note_job.go` | `VoiceNoteJob` type, job status constants, `NoteLink` |
| `voice_note_process.go` | `processVoiceNote` pipeline (transcribe→extract→notes) |
| `voice_note_cleanup.go` | Background goroutine to delete processed audio files after retention |
| `voice_note_jobs.go` | GET /voice-notes/jobs, POST /voice-notes/jobs/retry, POST /voice-notes/jobs/dismiss — voice note job list, retry, dismiss handlers |
| `tygo.yaml` | tygo config for Go→TypeScript type generation |

## Type Generation (Go → TypeScript)

[tygo](https://github.com/gzuidhof/tygo) generates `frontend/src/api-types.gen.ts` from Go structs with `json` tags. The frontend imports generated types instead of maintaining hand-written interfaces.

- Config: `backend/tygo.yaml`
- Generate: `cd backend && make generate`
- Check up-to-date: `cd backend && make check-types` (runs in root `make test`)
- Embedded struct flattening uses `tstype:",extends"` tags (see `ClassWithCount`, `ReportDetail`)
- `time.Time` maps to `string` via `type_mappings`

When changing Go structs with `json` tags, regenerate types and commit the updated `.gen.ts` file.

## Error Handling

`apiError` struct (`google.go`) carries HTTP status, machine-readable code, and human message. Handlers check `errors.As(err, &apiError)` and call `writeAPIError`. All responses are JSON.

## Testing

- Tests in `*_test.go` files override `serviceDeps` with stubs.
- `testutil_test.go` has shared test helpers (`stubVoiceNoteQueue`, `mockDepsAll`, etc.).
- `setupTestDB(t)` creates an in-memory SQLite DB with migrations for handler tests.
- Run: `make test` / `make lint`

## Environment Variables

| Variable | Required | Purpose |
|----------|----------|---------|
| `CLERK_SECRET_KEY` | Yes | Clerk Backend API key |
| `OPENAI_API_KEY` | Yes | Whisper transcription + GPT |
| `DB_PATH` | No | SQLite path (default `/data/gradebee.db`) |
| `UPLOADS_DIR` | No | Audio upload directory (default `/data/uploads`) |
| `UPLOAD_RETENTION_HOURS` | No | Hours to keep processed audio (default 168 = 7 days) |
| `ALLOWED_ORIGIN` | No | CORS origin (default `*`) |
| `PORT` | No | Local dev port (default `8080`) |
| `LOG_LEVEL` | No | DEBUG/INFO/WARN/ERROR/off |
| `LOG_FORMAT` | No | `json` for JSON, else text |
