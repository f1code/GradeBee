# Backend Architecture

## Overview

Go HTTP backend for GradeBee, a teacher tool for managing student rosters, processing audio recordings (upload → transcribe), and generating report cards. Runs as a standalone HTTP server.

**Deployment topology:** The Go binary embeds the React frontend via `embed.FS` and serves it directly. Dokku's nginx proxy handles TLS termination and gzip compression. No Caddy sidecar. See `Dockerfile` for the multi-stage build that copies the frontend `dist/` into `backend/static/` before the Go compile step.

**Package:** `handler` (all source files in `backend/` share this package).

**Storage:** SQLite database (`modernc.org/sqlite`) with WAL mode. Audio files stored on local disk. No Google Sheets/Docs — all data in SQLite.

## Entrypoint & Routing

**`handler.go`** — exports `Handle(w, r)`, the single HTTP handler. Routes use `strings.HasPrefix` + `pathParam()` for parameterized paths.

All API routes live under `/api/*`. The `Handle` function strips the `/api/` prefix, sets JSON `Content-Type` and CORS headers, then dispatches to handlers via a `switch` on the rewritten path. `/health` is exposed at the root (outside `/api/`) for uptime probes.

Anything else falls through to the embedded SPA handler (`spaHandler()` in `static.go`), which serves files from the embedded `static/` directory with `try_files`-style fallback to `index.html` for SPA client-side routing.

Cache headers:
- `/assets/*` (hashed filenames) → `Cache-Control: public, max-age=31536000, immutable`
- SPA fallback (`index.html`) → `Cache-Control: no-cache`

| Method | Path | Auth | Handler | Description |
|--------|------|------|---------|-------------|
| GET | `/` `/health` | No | inline | Health check |
| GET | `/api/classes` | Yes | `handleListClasses` | List user's classes with student counts |
| POST | `/api/classes` | Yes | `handleCreateClass` | Create a class (body: `{className, group}`) |
| PUT | `/api/classes/{id}` | Yes | `handleUpdateClass` | Update a class (body: `{className, group}`) |
| GET | `/api/classes/class-names` | Yes | `handleListClassNames` | Distinct class names for autocomplete |
| DELETE | `/api/classes/{id}` | Yes | `handleDeleteClass` | Delete class + cascade |
| GET | `/api/classes/{id}/students` | Yes | `handleListStudents` | List students in a class |
| POST | `/api/classes/{id}/students` | Yes | `handleCreateStudent` | Add a student |
| GET | `/api/students` | Yes | `handleGetStudents` | Full roster grouped by class |
| PUT | `/api/students/{id}` | Yes | `handleUpdateStudent` | Rename / move student |
| DELETE | `/api/students/{id}` | Yes | `handleDeleteStudent` | Delete student + cascade |
| GET | `/api/students/{id}/notes` | Yes | `handleListNotes` | List notes for a student |
| POST | `/api/students/{id}/notes` | Yes | `handleCreateNote` | Create a manual note |
| GET | `/api/students/{id}/aliases` | Yes | `handleListAliases` | List aliases for a student |
| POST | `/api/students/{id}/aliases` | Yes | `handleAddAlias` | Add an alias |
| DELETE | `/api/students/{id}/aliases/{aliasId}` | Yes | `handleRemoveAlias` | Remove an alias |
| GET | `/api/notes/{id}` | Yes | `handleGetNote` | Get single note |
| PUT | `/api/notes/{id}` | Yes | `handleUpdateNote` | Edit note summary |
| DELETE | `/api/notes/{id}` | Yes | `handleDeleteNote` | Delete a note |
| POST | `/api/reports` | Yes | `handleGenerateReports` | Generate report cards (returns HTML) |
| POST | `/api/reports/{id}/regenerate` | Yes | `handleRegenerateReport` | Regenerate with feedback |
| GET | `/api/students/{id}/reports` | Yes | `handleListReports` | List reports for a student |
| GET | `/api/reports/{id}` | Yes | `handleGetReport` | Get single report HTML |
| DELETE | `/api/reports/{id}` | Yes | `handleDeleteReport` | Delete a report |
| GET | `/api/report-examples` | Yes | `handleListReportExamples` | List example report cards |
| POST | `/api/report-examples` | Yes | `handleUploadReportExample` | Upload example report card |
| DELETE | `/api/report-examples` | Yes | `handleDeleteReportExample` | Delete example report card |
| POST | `/api/feedback` | Yes | `handleSubmitFeedback` | Submit explicit thumbs rating on a report or auto note |
| PUT | `/api/report-examples/{id}` | Yes | `handleUpdateReportExample` | Update example report card |
| POST | `/api/voice-notes/upload` | Yes | `handleUpload` | Upload audio to disk + dispatch job |
| POST | `/api/text-notes/upload` | Yes | `handleTextNotesUpload` | Submit pasted text + dispatch extraction job |
| POST | `/api/voice-notes/drive-import` | Yes | `handleDriveImport` | Download from Drive + dispatch job |
| GET | `/api/google-token` | Yes | `handleGoogleToken` | Return Google OAuth token for Drive Picker |
| GET | `/api/voice-notes/jobs` | Yes | `handleJobList` | List user's async upload jobs |
| POST | `/api/voice-notes/jobs/retry` | Yes | `handleJobRetry` | Retry failed jobs |
| POST | `/api/voice-notes/jobs/dismiss` | Yes | `handleJobDismiss` | Dismiss completed/failed jobs |

Auth is Clerk JWT via `clerkhttp.RequireHeaderAuthorization()` middleware. CORS handled inline (GET, POST, PUT, DELETE, OPTIONS).

## Async Upload Processing Pipeline

Audio uploads are processed asynchronously via a generic in-memory queue (`MemQueue[VoiceNoteJob]`) with a background worker pool. Jobs are dispatched from `POST /api/voice-notes/upload` and `POST /api/voice-notes/drive-import` after the file is saved to disk.

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
        │    → per-student observations (name, class, quoted_text, confidence)
        │    Note: quoted_text contains verbatim passages from the transcript.
        │    Stored in the notes table `summary` column (legacy name, no migration needed).
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
| `classes` | Teacher's classes (user_id, class_name, group_name, name) |
| `report_example_classes` | M-M link: report examples ↔ class names |
| `students` | Students belonging to classes |
| `student_aliases` | Nickname/variant aliases per student (per-class uniqueness, case-insensitive) |
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
| `cmd/server/main.go` | Server entrypoint; loads `.env`, inits Clerk, opens DB, runs migrations, starts queue + cleanup + HTTP. Supports `--migrate-only` flag (open DB, run migrations, exit 0) for Dokku predeploy hook. |
| `static.go` | Embeds `static/` (frontend dist, copied at Docker build time) via `embed.FS`; provides `spaHandler()` with SPA fallback and cache-control headers |
| `handler.go` | Routing, CORS, request logging, `Handle` entrypoint, `userIDFromRequest`, `pathParam` |
| `deps.go` | DI interface, prod implementations, `serviceDeps` variable |
| `google.go` | `apiError` type, `writeAPIError`, `newDriveReadClient` (Drive-read-only) |
| `auth.go` | `getGoogleOAuthToken` — Clerk → Google OAuth token |
| `db.go` | Open SQLite, set PRAGMAs (WAL, busy_timeout, foreign_keys) |
| `migrate.go` | Embed + run SQL migrations on startup |
| `sql/001_init.sql` | Schema: classes, students, notes, reports, report_examples, uploads (renamed to voice_notes via 002) |
| `sql/005_student_aliases.sql` | Migration: create `student_aliases` table with per-class uniqueness index |
| `sql/002_rename_uploads.sql` | Migration: rename uploads → voice_notes, update indexes |
| `repo_class.go` | `ClassRepo` — CRUD for classes |
| `repo_student.go` | `StudentRepo` — CRUD for students, `FindByNameAndClass` (matches canonical name + aliases, case-insensitive), `BelongsToUser`, `AddAlias`, `RemoveAlias`, `ListAliases`, `ListWithAliases` |
| `repo_note.go` | `NoteRepo` — CRUD for notes, `ListForStudents` (date range) |
| `repo_report.go` | `ReportRepo` — CRUD for reports |
| `repo_example.go` | `ReportExampleRepo` — CRUD for report examples |
| `repo_voice_note.go` | `VoiceNoteRepo` — CRUD for voice_notes, `MarkProcessed`, `ListStale` |
| `repo_errors.go` | `ErrNotFound`, `ErrDuplicate`, `isDuplicateErr`; `ErrDuplicateAlias` (carries `ConflictStudentName` for alias 409 responses) |
| `students.go` | GET /students, class/student CRUD handlers, `classGroup`/`student` types |
| `aliases.go` | GET/POST/DELETE /students/{id}/aliases — alias CRUD handlers |
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
| `logger.go` | Dual stdout+Sentry structured logging via `log/slog`; `InitLogger()` wires `slog.NewMultiHandler` when `SENTRY_DSN` is set; request-scoped logger via context |
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

`apiError` struct (`google.go`) carries HTTP status, machine-readable code, human message, and an optional `Details map[string]string` field for structured context (e.g. `conflictStudentName` on alias collision). Handlers check `errors.As(err, &apiError)` and call `writeAPIError`. All responses are JSON.

Repo-level errors:
- `ErrNotFound` — entity not found
- `ErrDuplicate` — generic unique constraint violation (used by class/student/note repos)
- `*ErrDuplicateAlias` — alias-specific conflict that carries the canonical name of the student who owns the conflicting alias, so the handler can include it in the 409 `details` field

## Observability / Sentry

`github.com/getsentry/sentry-go` v0.46.2. `InitSentry()` (`sentry.go`) reads `SENTRY_DSN` / `SENTRY_RELEASE` / `SENTRY_ENVIRONMENT` at startup — no-op if DSN is empty. `sentryhttp` middleware wraps the top-level handler in `main.go` (auto-captures panics; `Repanic: true`). Authenticated requests are tagged with the Clerk user ID. `BeforeSend` scrubs request bodies, query strings, cookies, auth headers, and name-shaped strings from exception values. `captureFeedback()` is available for non-error feedback events (task #19). DSN and release are baked into the Docker image via `VITE_SENTRY_DSN` / `VITE_APP_VERSION` build-args → `ENV SENTRY_DSN` / `ENV SENTRY_RELEASE` in Stage 3.

### Structured Logs

`InitLogger()` (`logger.go`) must be called after `InitSentry()`. When `SENTRY_DSN` is set it builds a `slog.NewMultiHandler` combining the stdout handler with a `sentryslog` handler (`github.com/getsentry/sentry-go/slog`). All `log.Info/Warn/Error` call sites are unchanged. Default `sentryslog` behaviour: `Debug`/`Info`/`Warn` → Sentry structured log entry only; `Error`/`Fatal` → structured log entry **and** a Sentry event (Issue).

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
| `SENTRY_DSN` | No | Sentry DSN; baked into Docker image via `VITE_SENTRY_DSN` build-arg |
| `SENTRY_RELEASE` | No | Release tag in Sentry; baked in via `VITE_APP_VERSION` build-arg (git SHA in prod) |
| `SENTRY_ENVIRONMENT` | No | Environment tag in Sentry (e.g. `production`, `staging`); set via `dokku config:set` |
| `EVAL_TOKEN` | No | Enables eval endpoints for `make eval` — **never set in production** |

---

## LLM Evaluation Harness

Regression testing for extraction and report-generation quality. On-demand only (`make eval`) — not CI-gated.

### Directory layout

```
backend/evals/
  promptfooconfig.yaml          promptfoo test suite
  baseline.json                 pinned baseline scores (committed to repo)
  scoring/extraction.js         custom JS precision/recall + voice-preservation scorer
  scripts/diff-baseline.js      baseline diff reporter (Node, always exits 0)
  results/                      per-run result JSONs (git-ignored)
  fixtures/
    extraction/<case>/
      transcript.txt            teacher audio transcript (synthetic)
      classes.json              class roster
      expected.json             expected students + must_quote_substrings
    reports/<case>/
      notes.json                student notes
      examples.json             example report cards (optional)
      instructions.txt          additional prompt instructions (optional)
```

### Running evals

```bash
# One-time eval — prints diff vs baseline
cd backend && make eval

# Update baseline after deliberate prompt/model change
cd backend && make eval-baseline   # runs eval then copies latest result to baseline.json
# Commit evals/baseline.json alongside the prompt change
```

### How to add a fixture

1. Create `backend/evals/fixtures/{extraction,reports}/<descriptive-name>/` with the required files (see layout above).
2. Add a test entry in `promptfooconfig.yaml` pointing at the new fixture.
3. Run `make eval` to see the score; if it looks right, run `make eval-baseline`.

### Baseline lifecycle

`backend/evals/baseline.json` is a single committed file overwritten by `make eval-baseline`. The PR diff is the audit trail — deliberately accepting new scores.

### Eval HTTP endpoints (gated by EVAL_TOKEN)

`eval_endpoint.go` registers two routes when `EVAL_TOKEN` is set:

| Route | Handler | Purpose |
|---|---|---|
| `POST /eval/extract` | `HandleEvalExtract` | Calls real extraction logic, no DB writes |
| `POST /eval/generate-report` | `HandleEvalGenerateReport` | Calls real report builder + GPT, no DB writes |

Both require `X-Eval-Token: <EVAL_TOKEN>` header. Missing/wrong token → 404 (not 401).

---

## User Feedback (artifact_feedback table)

Captures explicit thumbs ratings (👍/👎) and implicit signals (regenerate / edit / delete on auto notes) to feed a fixture-mining flywheel.

### Schema

```sql
CREATE TABLE artifact_feedback (
  id             INTEGER PRIMARY KEY,
  artifact_type  TEXT NOT NULL CHECK (artifact_type IN ('report', 'note')),
  artifact_id    INTEGER NOT NULL,
  rating         TEXT NOT NULL CHECK (rating IN ('up', 'down')),
  signal         TEXT NOT NULL DEFAULT 'explicit'
                 CHECK (signal IN ('explicit', 'regenerated', 'edited', 'deleted')),
  comment        TEXT,          -- explicit/regenerated signals
  previous_value TEXT,          -- edited/deleted signals (original content)
  user_id        TEXT NOT NULL,
  created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);
```

**Append-only** — code never UPDATEs rows, only INSERTs. Multiple edits → multiple rows.

### Signal taxonomy

| signal | rating | When inserted |
|---|---|---|
| `explicit` | `up`/`down` | User clicks 👍/👎 in `ReportViewer` or `NotesList` |
| `regenerated` | `down` | User clicks Regenerate on a report |
| `edited` | `down` | User edits an auto-extracted note |
| `deleted` | `down` | User deletes an auto-extracted note |

Only **explicit thumbs-down** events fire a Sentry dual-write (via `captureFeedback`).

### Prompt + model versioning

Every generated `report` row and auto-extracted `note` row is stamped with:
- `model_version` — the OpenAI model name (`ProductionModelName` const)
- `prompt_hash` — first 12 hex chars of SHA-256(`PromptVersionTag + ":" + template`) from `prompts_version.go`

`NULL` on pre-instrumentation rows. Filter `WHERE prompt_hash IS NOT NULL` when correlating quality.

`PromptVersionTag` is a manually-bumped monotonic integer for non-template logic changes.

---
