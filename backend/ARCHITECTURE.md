# Backend Architecture

## Overview

Go HTTP backend for GradeBee, a teacher tool for managing student rosters, processing audio recordings (upload → transcribe), and generating report cards. Runs as a standalone HTTP server.

**Deployment topology:** The Go binary embeds the React frontend via `embed.FS` and serves it directly. Dokku's nginx proxy handles TLS termination and gzip compression. No Caddy sidecar. See `Dockerfile` for the multi-stage build that copies the frontend `dist/` into `backend/static/` before the Go compile step.

**Package:** `handler` (all source files in `backend/` share this package).

**Storage:** SQLite database (`modernc.org/sqlite`) with WAL mode. Audio files stored on local disk. No Google Sheets/Docs — all data in SQLite.

## Domain Concepts

| Term | Definition |
|------|------------|
| **Level** | A Group-owned curriculum tier (e.g. "Grade 3", "Intermediate"), curated by the Admin via the Levels screen. Carries shared Report Instructions that will guide report generation. Referenced by Classes via `classes.level_id` (`NOT NULL`, `ON DELETE RESTRICT`). |
| **Day** | A mandatory weekday (`Monday`–`Sunday`) on every Class — the day of the class's first meeting of the week when a Level meets more than once. Enforced by a `CHECK` constraint on `classes.day`. |
| **Time slot** | An optional free-text label distinguishing sections taught at the same Level and Day (e.g. "Period 1", "Morning", "14:10"). |
| **Class** | A concrete teaching group — a **Level instance**. References a required Level (`level_id`), a required `day`, plus an optional `time_slot`. A teacher may have multiple classes at the same Level and Day (different time slots). The display name (`Class.Name`) is derived in SQL from the Level's name, Day (abbreviated to three letters), and `time_slot` (joined by ` · `, e.g. `Marcia · Wed` or `Marcia · Wed · 14:10`) — never stored — renaming a Level immediately renames every Class using it. |
| **Student** | A learner belonging to exactly one class. |
| **Note** | A per-student observation extracted from a voice or text upload. |
| **Report** | An LLM-generated report card for one student, drawing on their notes and the Level's Report Instructions. |

## Entrypoint & Routing

**`handler.go`** — exports `Handle(w, r)`, the single HTTP handler. Routes use `strings.HasPrefix` + `pathParam()` for parameterized paths.

All API routes live under `/api/*`. The `Handle` function strips the `/api/` prefix, sets JSON `Content-Type` and CORS headers, then dispatches to handlers via a `switch` on the rewritten path. `/health` is exposed at the root (outside `/api/`) for uptime probes.

Anything else falls through to the embedded SPA handler (`spaHandler()` in `static.go`), which serves files from the embedded `static/` directory with `try_files`-style fallback to `index.html` for SPA client-side routing.

Cache headers:
- `/assets/*` (hashed filenames) → `Cache-Control: public, max-age=31536000, immutable`
- `/manifest.json` → `Cache-Control: no-cache`
- SPA fallback (`index.html`) → `Cache-Control: no-cache`

| Method | Path | Auth | Handler | Description |
|--------|------|------|---------|-------------|
| GET | `/` `/health` | No | inline | Health check |
| GET | `/api/classes` | Yes | `handleListClasses` | List user's classes with student counts |
| POST | `/api/classes` | Yes | `handleCreateClass` | Create a class (body: `{levelId, day, timeSlot}`) |
| PUT | `/api/classes/{id}` | Yes | `handleUpdateClass` | Update a class (body: `{levelId, day, timeSlot}`) |
| DELETE | `/api/classes/{id}` | Yes | `handleDeleteClass` | Delete class + cascade |
| GET | `/api/classes/{id}/students` | Yes | `handleListStudents` | List students in a class |
| POST | `/api/classes/{id}/students` | Yes | `handleCreateStudent` | Add a student |
| GET | `/api/students` | Yes | `handleGetStudents` | Full roster grouped by class |
| PUT | `/api/students/{id}` | Yes | `handleUpdateStudent` | Rename / move student. Move body: `{classId}`; response adds `droppedAliases: string[]` (omitted when empty) if any of the student's aliases collided with the target class and were dropped. A canonical-name collision in the target class aborts the move with 409 (`student_name_conflict`, `details.conflictStudentName`) |
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
| POST | `/api/feedback` | Yes | `handleSubmitFeedback` | Submit explicit thumbs rating on a report or auto note |
| POST | `/api/voice-notes/upload` | Yes | `handleUpload` | Upload audio to disk + dispatch job |
| POST | `/api/text-notes/upload` | Yes | `handleTextNotesUpload` | Submit pasted text + dispatch extraction job |
| POST | `/api/voice-notes/drive-import` | Yes | `handleDriveImport` | Download from Drive + dispatch job |
| GET | `/api/google-token` | Yes | `handleGoogleToken` | Return Google OAuth token for Drive Picker |
| GET | `/api/voice-notes/jobs` | Yes | `handleJobList` | List user's async upload jobs |
| POST | `/api/voice-notes/jobs/retry` | Yes | `handleJobRetry` | Retry failed jobs |
| POST | `/api/voice-notes/jobs/dismiss` | Yes | `handleJobDismiss` | Dismiss completed/failed jobs |
| GET | `/api/levels` | Yes | `handleListLevels` | List the caller's Group's Levels |
| POST | `/api/levels` | Yes (Admin) | `handleCreateLevel` | Create a Level (body: `{name}`) |
| PUT | `/api/levels/{id}` | Yes (Admin) | `handleUpdateLevel` | Rename and/or set Report Instructions (body: `{name?, reportInstructions?}`) |
| DELETE | `/api/levels/{id}` | Yes (Admin) | `handleDeleteLevel` | Delete a Level; refused with 409 (message states the count) if any Class still references it |

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
        │    Read audio from local disk → LLM provider (Voxtral or Whisper)
        │    Context bias seeded with class names from DB roster
        │
        ├─ Step 2: Extract (status → "extracting")
        │    Send transcript + student roster to LLM provider
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

Audio files are deleted from disk **immediately after successful transcription** in `voice_note_process.go`. The `purged_at` column on `voice_notes` records when this happened. The background cleanup goroutine (`voice_note_cleanup.go`) then removes the DB row after the retention period (default 7 days, configurable via `UPLOAD_RETENTION_HOURS`), skipping file deletion for rows already purged.

### Generic Queue Infrastructure

The queue system uses Go generics for type safety:

- **`Keyed`** — constraint interface requiring `JobKey() string` and `OwnerID() string`
- **`JobQueue[T Keyed]`** — generic interface for async job operations (Publish, GetJob, UpdateJob, ListJobs, DeleteJob, Close)
- **`MemQueue[T Keyed]`** — in-memory implementation with buffered channel + worker pool
- **`ProcessFunc[T Keyed]`** — function type called by workers: `func(ctx, queue, key) error`

Each job type gets its own queue instance. The processor function is injected at construction via closure, keeping the generic queue status-agnostic.

## Dependency Injection

**`deps.go`** — defines `deps` interface + `prodDeps` implementation + package-level `serviceDeps` variable.

```
deps interface {
    GetTranscriber()      → Transcriber
    GetRoster(ctx, userID) → Roster
    GetExtractor()        → Extractor
    GetNoteCreator()      → NoteCreator
    GetReportGenerator()  → ReportGenerator
    GetVoiceNoteQueue()   → JobQueue[VoiceNoteJob]
    GetDriveClient(ctx, userID) → DriveClient
    GetDB()               → *sql.DB
    GetClassRepo()        → *ClassRepo
    GetStudentRepo()      → *StudentRepo
    GetNoteRepo()         → *NoteRepo
    GetReportRepo()       → *ReportRepo
    GetVoiceNoteRepo()    → *VoiceNoteRepo
    GetLevelRepo()        → *LevelRepo
    GetUploadsDir()       → string
}
```

Tests override `serviceDeps` with stubs. All handler functions call through this interface, never instantiate services directly.

### Key Interfaces

| Interface | File | Prod Implementation | Purpose |
|-----------|------|---------------------|---------|
| `deps` | `deps.go` | `prodDeps` | Top-level DI container |
| `Roster` | `roster.go` | `dbRoster` | Read student data from DB |
| `Transcriber` | `transcriber.go` | `providerTranscriber` | Audio→text via LLMProvider (Voxtral or Whisper) |
| `Extractor` | `extract.go` | `llmExtractor` | Transcript→student extraction via LLMProvider |
| `NoteCreator` | `notes.go` | `dbNoteCreator` | Create notes in SQLite |
| `ReportGenerator` | `report_generator.go` | `llmReportGenerator` | LLM-based report card generation (HTML output) |
| `JobQueue[VoiceNoteJob]` | `job_queue.go` | `MemQueue[VoiceNoteJob]` | Generic in-memory async job queue with worker pool |

## External Services

### Google OAuth (`google.go`)
- Auth: Clerk JWT → extract user ID → Google OAuth token (used for Drive Picker import).
- **Note:** Google Drive was replaced by SQLite as the primary data store. Drive Picker import remains active for importing audio files from a user's own Drive.

### Clerk (`auth.go`)
- JWT verification via middleware.
- OAuth token retrieval: `user.ListOAuthAccessTokens` for `oauth_google`.
- `userIDFromRequest(r)` extracts user ID from Clerk session claims (in `handler.go`).
- `groupIDFromRequest(r)` — extracts the active Clerk Organization ID (`ActiveOrganizationID`) from verified session claims. Returns `403 unauthorized` if claims are absent, `403 no_active_org` if no org is active (user not yet in a Group).
- `isAdmin(r)` — returns `true` if the session role is `"org:admin"` (uses `SessionClaims.HasRole`).
- `debugAuthMiddleware` enforces the active-org gate after Clerk JWT verification: any verified request with an empty `ActiveOrganizationID` is rejected with `403 no_active_org` before reaching the handler. `/health` and static routes are outside this middleware.
- **Phase 2:** `levels` table is Group-owned and scoped by `group_id` (the active Clerk Organization ID). `LevelRepo` (`repo_level.go`) enforces the Group boundary on every method; `/api/levels` reads are open to any Group member, writes (`POST`/`PUT`/`DELETE`) require `isAdmin(r)`. `classes.level_id` wires Classes to Levels; `ClassRepo.Create`/`Update` validate `level_id` belongs to the caller's Group, rejecting a forged cross-Group reference.

### LLM Provider (`llm_provider*.go`)

A `LLMProvider` interface abstracts all LLM call sites. Two production implementations exist:
- `openaiProvider` — wraps `go-openai` client against `https://api.openai.com/v1` (chat/vision) and OpenAI Whisper (transcription).
- `mistralProvider` — wraps `go-openai` client against `https://api.mistral.ai/v1` (chat/vision via OpenAI-compat endpoint) and ZaguanLabs `mistral-go/v2/sdk` for Voxtral transcription.

`LoadProvider()` reads `LLM_PROVIDER` (default `"mistral"`), validates the active provider's API key, logs the selected models, and returns the provider. Called from `NewProdDeps` so the binary fails fast on misconfiguration.

Per-task model IDs are configured via `LLM_MODEL_EXTRACTION`, `LLM_MODEL_REPORT`, `LLM_MODEL_VISION`, `LLM_MODEL_TRANSCRIPTION` env vars (provider-specific defaults apply if unset).


Context bias: `providerTranscriber` passes class names from the DB roster to `provider.Transcribe(...)`. `openaiProvider` joins them as a Whisper prompt; `mistralProvider` sanitises them (space→`_`, drop commas, dedup, cap 100) and passes via Voxtral's `context_bias` field.

### Audio format handling (`transcriber.go`, `audio_format.go`)
- Handles audio format detection and 3GP→MP4 patching (`audio_format.go`).

## Database

SQLite with WAL mode (`db.go`). Migrations in `sql/` are embedded via `embed.FS` and applied in lexical filename order (`migrate.go`).

### Tables

| Table | Purpose |
|-------|---------|
| `classes` | A **class** is a Level instance: a required `level_id` FK (`NOT NULL`, `ON DELETE RESTRICT`), a required `day` (`NOT NULL`, `CHECK` over the seven weekday names), plus an optional `time_slot`. `Class.Name` and `Class.LevelName` are not stored — both are derived in SQL by joining `levels` (`levels.name`, ` · ` + Day abbreviated to three letters, and ` · ` + `time_slot` when set), so renaming a Level immediately renames every Class using it. Uniqueness is `(user_id, level_id, day, time_slot)`. |
| `students` | Students belonging to classes |
| `student_aliases` | Nickname/variant aliases per student (per-class uniqueness, case-insensitive) |
| `notes` | Observation notes per student |
| `reports` | Generated HTML report cards |
| `voice_notes` | Audio file tracking (file path, processed_at, purged_at) |
| `levels` | Group-owned curriculum tiers. `name` unique within `group_id`; `report_instructions` defaults to `''`. A Level with trimmed-empty `report_instructions` cannot generate or regenerate reports — `handleGenerateReports`/`handleRegenerateReport` refuse with `400` before any LLM call (see `report_prompt.go`/`reports_handler.go` below). Seeded with 8 hand-authored Levels against the production Clerk org ID. |

### Repository Layer

Each table has a `Repo*` type in `repo_*.go` files providing type-safe CRUD.

## Authorization Pattern

All CRUD endpoints verify resource ownership:
1. Extract `userID` from Clerk JWT claims via `userIDFromRequest(r)`
2. Extract `groupID` from the active Clerk Organisation via `groupIDFromRequest(r)` (available from Phase 1; used for scoped queries from Phase 2 onward)
3. For class operations: query class, check `class.UserID == userID`
4. For student operations: `studentRepo.BelongsToUser(studentID, userID)`
5. For note/report operations: join through student → class to verify ownership

The `debugAuthMiddleware` enforces that every `/api/` request carries an active Clerk Organization (`ActiveOrganizationID != ""`). Requests without an active org receive `403 no_active_org` before reaching any handler.

## File-by-File Reference

| File | Responsibility |
|------|---------------|
| `cmd/server/main.go` | Server entrypoint; loads `.env`, inits Clerk, opens DB, runs migrations, starts queue + cleanup + HTTP. Supports `--migrate-only` flag (open DB, run migrations, exit 0) for Dokku predeploy hook. |
| `static.go` | Embeds `static/` (frontend dist, copied at Docker build time) via `embed.FS`; provides `spaHandler()` with SPA fallback and cache-control headers |
| `handler.go` | Routing, CORS, request logging, `Handle` entrypoint, `userIDFromRequest`, `pathParam` |
| `deps.go` | DI interface, prod implementations, `serviceDeps` variable |
| `llm_provider.go` | `LLMProvider` interface, request/response types, `LLMTask` enum, `LoadProvider()` factory |
| `llm_provider_openai.go` | `openaiProvider` — OpenAI chat/vision via go-openai + Whisper transcription |
| `llm_provider_mistral.go` | `mistralProvider` — Mistral chat/vision via OpenAI-compat endpoint + Voxtral transcription via ZaguanLabs SDK |
| `google.go` | `apiError` type, `writeAPIError`, `newDriveReadClient` (Drive-read-only) |
| `auth.go` | `groupIDFromRequest`, `isAdmin` — Clerk org/role helpers; `getGoogleOAuthToken` — Clerk → Google OAuth token |
| `db.go` | Open SQLite, set PRAGMAs (WAL, busy_timeout, foreign_keys) |
| `migrate.go` | Embed + run SQL migrations on startup |
| `sql/` | Embedded SQL migrations; applied in lexical filename order and tracked in `_migrations` |
| `repo_class.go` | `ClassRepo` — CRUD for classes, scoped by `group_id` on Create/Update to validate `level_id` belongs to the caller's Group |
| `repo_student.go` | `StudentRepo` — CRUD for students, `FindByNameAndClass` (matches canonical name + aliases, case-insensitive), `BelongsToUser`, `AddAlias`, `RemoveAlias`, `ListAliases`, `ListWithAliases`. `Move` is transactional: updates `students.class_id` and re-homes `student_aliases.class_id` together, aborting on a canonical-name collision in the target class (`*ErrDuplicateStudentName`) and silently dropping (not blocking on) any of the student's aliases that collide with the target class's names/aliases |
| `repo_note.go` | `NoteRepo` — CRUD for notes, `ListForStudents` (date range) |
| `repo_report.go` | `ReportRepo` — CRUD for reports |
| `repo_voice_note.go` | `VoiceNoteRepo` — CRUD for voice_notes, `MarkProcessed`, `MarkPurged`, `ListStale` |
| `repo_level.go` | `LevelRepo` — CRUD for levels, every method scoped by `group_id` |
| `levels.go` | GET/POST/PUT/DELETE /levels handlers — write endpoints gated on `isAdmin(r)` |
| `repo_errors.go` | `ErrNotFound`, `ErrDuplicate`, `isDuplicateErr`; `ErrDuplicateAlias` (carries `ConflictStudentName` for alias 409 responses) |
| `students.go` | GET /students, class/student CRUD handlers, `classGroup`/`student` types |
| `aliases.go` | GET/POST/DELETE /students/{id}/aliases — alias CRUD handlers |
| `roster.go` | `Roster` interface + `dbRoster` — DB-backed roster reads |
| `voice_note_upload.go` | POST /voice-notes/upload — multipart audio → disk + voice_notes table + dispatch job |
| `transcriber.go` | `Transcriber` interface + `providerTranscriber` (delegates to LLMProvider) |
| `voice_note_drive_import.go` | POST /voice-notes/drive-import — download from Drive → disk + voice_notes table + dispatch job |
| `google_token.go` | GET /google-token — return user's Google OAuth access token |
| `extract.go` | `Extractor` interface + `llmExtractor` for transcript analysis |
| `notes.go` | `NoteCreator` interface + `dbNoteCreator`, note CRUD handlers |
| `report_generator.go` | `ReportGenerator` interface + `llmReportGenerator` (HTML output) |
| `report_prompt.go` | GPT prompt construction for report generation. `BuildReportPrompt` emits ranked sections: the Level's Report Specification (mandatory), then ad-hoc instructions (override the Specification where they conflict), then Student Notes (sole source of facts), then feedback. Requests HTML output. |
| `reports_handler.go` | POST /reports, POST /reports/{id}/regenerate, report CRUD handlers. Both generation endpoints pre-flight-resolve every selected student's Class → Level and refuse the whole request with `400` (naming the offending Levels) if any Level's `report_instructions` is trimmed-empty — no report row created, no LLM call made. |
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
- `*ErrDuplicateStudentName` — returned by `StudentRepo.Move` when the student's canonical name collides with a name or alias already in the target class; wraps `ErrDuplicate` (`Unwrap`) so generic `errors.Is(err, ErrDuplicate)` checks still work, while `errors.As` recovers the conflicting name for the 409 `details.conflictStudentName` field
- `*ErrLevelInUse` — carries the count of Classes still referencing a Level being deleted, so `handleDeleteLevel` can return a 409 stating how many Classes must move first; the DB's `ON DELETE RESTRICT` on `classes.level_id` backs this up but can't name the count itself

## Observability / Sentry

`github.com/getsentry/sentry-go` v0.46.2. `InitSentry()` (`sentry.go`) reads `SENTRY_DSN` / `SENTRY_RELEASE` / `SENTRY_ENVIRONMENT` at startup — no-op if DSN is empty. `sentryhttp` middleware wraps the top-level handler in `main.go` (auto-captures panics; `Repanic: true`). Authenticated requests are tagged with the Clerk user ID. `BeforeSend` scrubs request bodies, query strings, cookies, auth headers, and name-shaped strings from exception values. `captureFeedback()` is available for non-error feedback events (task #19). DSN, release, and environment are baked into the Docker image via `VITE_SENTRY_DSN` / `VITE_APP_VERSION` / `VITE_SENTRY_ENVIRONMENT` build-args.

### Structured Logs

`InitLogger()` (`logger.go`) must be called after `InitSentry()`. When `SENTRY_DSN` is set it builds a `slog.NewMultiHandler` combining the stdout handler with a `sentryslog` handler (`github.com/getsentry/sentry-go/slog`). All `log.Info/Warn/Error` call sites are unchanged. Default `sentryslog` behaviour: `Debug`/`Info`/`Warn` → Sentry structured log entry only; `Error`/`Fatal` → structured log entry **and** a Sentry event (Issue).

**No student names in log attributes or error strings** — see
[ADR 0003](../docs/adr/0003-no-child-pii-in-telemetry.md). Log `student_id`, or the job key on the
voice-note paths, and let the reader resolve it against the DB. `BeforeSend`'s name-shaped-string
scrubbing (above) is not a backstop for this: it only inspects exception values, never log
attributes, and it misses single first names and non-ASCII ones. New telemetry on a path that
touches a student is expected to carry a test asserting the name's absence, as
`TestProcessJob_DropSitesOmitStudentName` and `TestProcessJob_FailurePathsOmitStudentName` do.

## Testing

- Tests in `*_test.go` files override `serviceDeps` with stubs.
- `testutil_test.go` has shared test helpers (`stubVoiceNoteQueue`, `mockDepsAll`, etc.).
- `setupTestDB(t)` creates an in-memory SQLite DB with migrations for handler tests.
- Run: `make test` / `make lint`

## Environment Variables

| Variable | Required | Purpose |
|----------|----------|---------|
| `CLERK_SECRET_KEY` | Yes | Clerk Backend API key |
| `LLM_PROVIDER` | No | `"openai"` or `"mistral"` (default: `"mistral"`) — selects the LLM backend |
| `OPENAI_API_KEY` | When `LLM_PROVIDER=openai` | OpenAI API key (chat, vision, Whisper) |
| `MISTRAL_API_KEY` | When `LLM_PROVIDER=mistral` | Mistral API key (chat, vision, Voxtral) |
| `LLM_MODEL_EXTRACTION` | No | Extraction model ID (default: `mistral-medium-2508` / `gpt-5.4-mini`) |
| `LLM_MODEL_REPORT` | No | Report generation model ID |
| `LLM_MODEL_VISION` | No | Vision model ID |
| `LLM_MODEL_TRANSCRIPTION` | No | Transcription model ID (default: `voxtral-mini-latest` / `whisper-1`) |
| `DB_PATH` | No | SQLite path (default `/data/gradebee.db`) |
| `UPLOADS_DIR` | No | Audio upload directory (default `/data/uploads`) |
| `UPLOAD_RETENTION_HOURS` | No | Hours to keep processed audio (default 168 = 7 days) |
| `ALLOWED_ORIGIN` | No | CORS origin (default `*`) |
| `PORT` | No | Local dev port (default `8080`) |
| `LOG_LEVEL` | No | DEBUG/INFO/WARN/ERROR/off |
| `SENTRY_DSN` | No | Sentry DSN; baked into Docker image via `VITE_SENTRY_DSN` build-arg |
| `SENTRY_RELEASE` | No | Release tag in Sentry; baked in via `VITE_APP_VERSION` build-arg (git SHA in prod) |
| `SENTRY_ENVIRONMENT` | No | Environment tag in Sentry (`production` / `review` / `development`); baked in via the `VITE_SENTRY_ENVIRONMENT` build-arg, same value as the frontend. Override at runtime with `dokku config:set` if needed |
| `EVAL_MODEL` | No | ~~Override OpenAI model for `make eval`~~ — removed; model selection now lives in `promptfooconfig.yaml` (`providers[].id`) |

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
      report_instructions.txt   Level's report specification (structure/sections; drives content).
      instructions.txt          ad-hoc per-run instructions (optional; override report_instructions
                                 where they conflict)
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

### How it works

`make eval` builds `bin/eval-cli` (from `cmd/eval-cli/`), then invokes promptfoo. For each test case, promptfoo calls eval-cli as an **exec-prompt function** (passing a single JSON arg), and eval-cli returns a messages array — no LLM call. Promptfoo sends the messages to its native OpenAI provider (with structured output schema for extraction), scores the response, and writes results. Model selection lives entirely in `promptfooconfig.yaml`; `EVAL_MODEL` is no longer read.

Debug a single case:
```bash
cd backend
make bin/eval-cli
./bin/eval-cli '{"vars":{"transcript":"Alice","classes":[]},"config":{"task":"build-extract-prompt"}}'
```

### Eval CLI (`cmd/eval-cli`)

| Config task | Reads from `vars` | Builds |
|---|---|---|
| `build-extract-prompt` | `transcript`, `classes` | `BuildExtractionPrompt` → messages array (system + user) |
| `build-report-prompt` | `student_name`, `class`, `notes`, `report_instructions`, `instructions` | `BuildReportPrompt` → messages array (user only) |

Model selection and the actual LLM call belong to promptfoo, not eval-cli.

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
- `model_version` — the LLM model ID that produced the note (e.g. `"mistral-medium-2508"`); raw model ID, no provider prefix; `NULL` for manually-created notes
- `prompt_hash` — first 12 hex chars of SHA-256(`PromptVersionTag + ":" + template`) from `prompts_version.go`

`NULL` on pre-instrumentation rows. Filter `WHERE prompt_hash IS NOT NULL` when correlating quality.

`PromptVersionTag` is a manually-bumped monotonic integer for non-template logic changes.

---
