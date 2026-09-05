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

**`handler.go`** — exports `Handle(w, r)`, the single HTTP handler. It attaches the request-scoped logger and `X-Request-ID`, serves `/health`, falls through to the SPA for non-`/api/` paths, sets JSON `Content-Type` and CORS headers (answering `OPTIONS` itself), and hands every `/api/` request to `apiMux`.

**`router.go`** — `newAPIMux(auth)` registers each route on a Go 1.22+ `http.ServeMux` with a method+pattern string (`"PUT /api/classes/{id}"`, `"DELETE /api/students/{id}/aliases/{aliasID}"`), wrapping every handler in `auth` — `clerkAuthMiddleware` in production, `fakeAuth` in tests. Handlers read wildcards with `idParam(r, "id")` (`r.PathValue` parsed to int64). A `"/api/"` catch-all writes the JSON `{"error":"not found"}` 404, and because it always matches, a wrong method on a known path is that same 404 rather than ServeMux's text 405. Patterns match exactly, so `GET /api/notes/5/extra` is a 404 too. `/health` is exposed at the root (outside `/api/`) for uptime probes.

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
| POST | `/api/voice-notes/{uploadId}/assemble` | Yes | `handleAssembleNotes` | File a recording under a class the teacher picked and make its notes; runs extraction's second pass against that class (body: `{className}`) |
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
        │    Delete the audio file and set purged_at, then write the
        │    transcript to voice_notes.transcript (text jobs too);
        │    a failed write fails the job (retry skips transcription)
        │
        ├─ Step 2: Extract (status → "extracting"), two LLM calls
        │    Pass 1: the teacher's class list alone → the class this
        │      recording is about (enum of their class names plus "")
        │    "" is the decline: no header, or a header naming two
        │      classes. Pass 2 does not run, the job completes with no
        │      notes and reason class_unclear, and the card offers the
        │      class picker. Not a failure — a failed card offers retry
        │    Pass 2: the transcript against that one class's roster
        │      → passages (kind, spoken_labels, student, summary)
        │    A child passage whose spoken labels are all pronouns is
        │      demoted to unknown before anything reads its student
        │
        ├─ Step 3: Create Notes (status → "creating_notes")
        │    Fold passages into one note per child (voice_note_passages.go):
        │      child + roster student → that child's note, in spoken order
        │      child with no student, unknown → nobody; stays on the card
        │      group → every child this recording already reached
        │      none  → dropped, and kept off the card entirely
        │    Resolve name → student ID via FindByNameAndClass
        │    Create note in SQLite via dbNoteCreator
        │
        └─ Done (status → "done", mark voice note processed)
```

On failure at any step, the job status is set to `"failed"` with the error message. Users can retry failed jobs via `POST /voice-notes/jobs/retry`.

Job status is tracked in-memory (map keyed by `userId/<uploadId>`). The frontend polls `GET /voice-notes/jobs` to show progress.

### Startup

`cmd/server/main.go` calls `InitVoiceNoteQueue(d, 4)` at startup to create the queue with 4 worker goroutines. The queue is shut down gracefully on SIGINT/SIGTERM.

### The class picker (`voice_note_assemble.go`)

Two recordings finish `done` with no notes and a class that could still rescue them, and one endpoint serves both. `class_unclear` is a decline: pass 1 could not place the recording, so pass 2 never ran. `no_name_matched` is a recording read against the wrong sibling class: pass 2 ran, against the wrong roster. `nobody_named` cannot be rescued, because spoken names are read off the transcript and no class the teacher picks can make one appear.

The card does not read those reasons to decide whether to offer `ClassPicker` — it obeys `canPickClass`, computed by the pipeline beside the reason and carried on the job and on the assemble response. `noNotesReason` says why; `canPickClass` says what can be done about it. Folding the two made the card derive an affordance from a list of the causes it happened to know, so a new cause silently removed the picker, and it forced the assemble handler to name a cause it could not know in order to keep the picker up.

`POST /api/voice-notes/{uploadId}/assemble` takes `{className}` and runs extraction's second pass itself (`Extractor.ExtractPassages`), then folds it with `assemblePassages` — the same pass and the same fold the pipeline uses, so a recording has one resolution path however it was filed. The body carries no passages: the summaries become the note text under `source = reviewed` stamped with the model's id, so they must be words the model wrote in that request.

The order the handler runs in is load-bearing:

- **Both ownership gates first**, outside the lock. A caller probing another tenant's upload id gets its 404 without spending a model call or taking a lock.
- **The lock is in-process**, so it guards against a double-submit only while the API runs as a single process. Two of them and every child gets two identical notes again, with nothing failing and nothing logged. The in-memory job queue has the same property, but that one is a cache with understood data loss; this is a correctness guard, and moving the API to more than one instance means moving this to a shared one.
- **One job read, inside the lock.** The race window is job-read to `UpdateJob` with no re-read between, so reading outside would leave the window the lock exists to close. A second pick on the same recording gets 409 immediately rather than waiting out the model call.
- **Pass 2 before the first `CreateNote`**, under a 30s deadline of its own (not the handler's, which would cancel the note loop mid-write). A provider error returns with nothing written and the card keeps its picker.

A pick writes the job only when it made notes. `assembleOutcome` decides what the teacher is told, and it is a pure function given no access to what pass 2 returned — deliberately, because a reason read off that run would sooner or later close a picker that should have stayed open. Its three outcomes: notes created, answer with them; no notes and a job to read, answer with the job unchanged, so a refresh does not contradict the card; no notes and a forgotten job, claim no class and name no cause, but keep the picker up. That last one matters more than it looks — the card keeps a forgotten job's done card and this response outlives the poll, so a wrong answer there is permanent in that tab.

That includes a pick whose passages carry no spoken name. It is tempting to end the picker there ("no class the teacher picks can make a name appear"), and wrong: pass 2 against the wrong roster is instructed to return a name fitting no listed child as `unknown` with an empty `spoken_labels`, which is indistinguishable here from a recording that named nobody. Ending on it would strand the wrong-sibling-class recording this endpoint exists for, and a declined recording has no earlier passages to fall back on. The pipeline may act on that reason because it has the pinned class; this handler may not. A recording that genuinely named nobody never reaches here anyway — the card offers the picker on `class_unclear` and `no_name_matched` only.

Still open, both pre-existing: a failure part-way through the note loop leaves notes created with the job never updated, and after a restart there is no job to check at all, so the three refusals and the in-process lock both fall away. The fix for either is a passages table.

### Voice Note Cleanup

Audio files are deleted from disk **immediately after successful transcription** in `voice_note_process.go`. The `purged_at` column on `voice_notes` records when this happened. The background cleanup goroutine (`voice_note_cleanup.go`) then removes the DB row after the retention period (default 7 days, configurable via `UPLOAD_RETENTION_HOURS`), skipping file deletion for rows already purged. The period counts from `processed_at` for a job that finished or was dismissed, and from `created_at` for one that never did — a failed job nobody retried, or one lost to a restart — so no row outlives the window. `MarkProcessed` keeps the first timestamp, so dismissing an already-finished job does not restart the clock.

The transcript is written to `voice_notes.transcript` right after the audio is deleted, and whether or not extraction goes on to create a note — until then, `notes.transcript` was the only copy, so a job that created no note left nothing behind. The audio goes first so no failure after it can leave it on disk: the privacy page promises the recording is deleted immediately after transcription. A failed delete itself is only warned about; the retention cleanup removes the file later, once the job completes or the teacher dismisses it. A failed transcript write fails the job; the job struct keeps the transcript, so a retry skips transcription and repeats only the write. That window closes at process restart: the job queue is in memory, so a failed write followed by a deploy before the teacher retries loses the recording for good. `purged_at` marks the audio alone; the transcript stays on the row and is deleted with it by the cleanup goroutine. The assemble endpoint reads it: it is the only source of the transcript stored on every reviewed note, and a recording whose row has been emptied is refused with 404 rather than filed with none.

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
| `Extractor` | `extract.go` | `llmExtractor` | Transcript→passages, in two LLM calls: the class, then that class's children. `ExtractPassages` runs the second alone, for a class the caller already has |
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
- `clerkAuthMiddleware` enforces the active-org gate after Clerk JWT verification: any verified request with an empty `ActiveOrganizationID` is rejected with `403 no_active_org` before reaching the handler. `/health` and static routes are outside this middleware.
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
| `notes` | Observation notes per student. `source` is `auto` (written by the pipeline end to end), `reviewed` (the model wrote the text, the teacher supplied only the class — see the assemble endpoint) or `manual` (typed by the teacher). The column has no `CHECK`, so the `NoteSource*` constants in `notes.go` are the contract; `auto` and `reviewed` are both model-written and both fire the implicit thumbs-down on edit or delete. |
| `reports` | Generated HTML report cards |
| `voice_notes` | Audio file tracking (file path, processed_at, purged_at) plus the `transcript`, written before the audio is deleted and kept for the row's lifetime |
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

Steps 4 and 5 go through `requireStudentOwnership(w, r, studentID, userID, notFoundMsg)`
in `handler.go` — the single gate for "does this caller own this student", used by all
sixteen call sites. It writes the 404 itself and returns `false`, so handlers read:

```go
if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
    return
}
```

It exists to keep two events apart that a bare `if err != nil || !owns` collapses into one
silent 404. A check that *could not run* is an outage and logs at Error; a check that ran
and *said no* is a denial and logs at Warn — queryable, deliberately not paging, since the
false-positive rate from a delete race against a stale client roster is unknown. Both arms
write the identical response, so the caller still cannot tell an outage from a denial from
a genuine miss.

`notFoundMsg` is the whole caller-facing string rather than a noun, because
`handleGenerateReports` echoes back the student name the caller supplied. That name is
never logged: telemetry carries `student_id` only (`docs/adr/0003`).

Each record is labelled with its handler via `callerName()` (an `op` field), not with
`r.URL.Path`. The router 404s a stray trailing segment before any handler runs, but the
path is still client-controlled text — logging it would let a caller plant a child's
name in telemetry. The handler name cannot carry caller input. Denials land at Warn and
production runs at `INFO` (`ansible/vars.yml`), so they are retained.

The `clerkAuthMiddleware` enforces that every `/api/` request carries an active Clerk Organization (`ActiveOrganizationID != ""`). Requests without an active org receive `403 no_active_org` before reaching any handler.

## File-by-File Reference

| File | Responsibility |
|------|---------------|
| `cmd/server/main.go` | Server entrypoint; loads `.env`, inits Clerk, opens DB, runs migrations, starts queue + cleanup + HTTP. Supports `--migrate-only` flag (open DB, run migrations, exit 0) for Dokku predeploy hook. |
| `static.go` | Embeds `static/` (frontend dist, copied at Docker build time) via `embed.FS`; provides `spaHandler()` with SPA fallback and cache-control headers |
| `handler.go` | `Handle` entrypoint, CORS, request logging, `clerkAuthMiddleware`, `userIDFromRequest`, `requireStudentOwnership`, `callerName`/`callerAt` |
| `router.go` | `newAPIMux` route table (`http.ServeMux` method+pattern strings), `idParam`, JSON 404 catch-all |
| `deps.go` | DI interface, prod implementations, `serviceDeps` variable |
| `llm_provider.go` | `LLMProvider` interface, request/response types, `LLMTask` enum, `LoadProvider()` factory |
| `llm_provider_openai.go` | `openaiProvider` — OpenAI chat/vision via go-openai + Whisper transcription |
| `llm_provider_mistral.go` | `mistralProvider` — Mistral chat/vision via OpenAI-compat endpoint + Voxtral transcription via ZaguanLabs SDK |
| `errors_http.go` | `apiError` type, `writeAPIError`, `writeError`, `writeInternalError` — the error-response contract |
| `google.go` | `newDriveReadClient` (Drive-read-only) |
| `auth.go` | `groupIDFromRequest`, `isAdmin` — Clerk org/role helpers; `getGoogleOAuthToken` — Clerk → Google OAuth token |
| `db.go` | Open SQLite, set PRAGMAs (WAL, busy_timeout, foreign_keys) |
| `migrate.go` | Embed + run SQL migrations on startup |
| `sql/` | Embedded SQL migrations; applied in lexical filename order and tracked in `_migrations` |
| `repo_class.go` | `ClassRepo` — CRUD for classes, scoped by `group_id` on Create/Update to validate `level_id` belongs to the caller's Group |
| `repo_student.go` | `StudentRepo` — CRUD for students, `FindByNameAndClass` (matches canonical name + aliases, case-insensitive), `BelongsToUser`, `AddAlias`, `RemoveAlias`, `ListAliases`, `ListWithAliases`. `Move` is transactional: updates `students.class_id` and re-homes `student_aliases.class_id` together, aborting on a canonical-name collision in the target class (`*ErrDuplicateStudentName`) and silently dropping (not blocking on) any of the student's aliases that collide with the target class's names/aliases |
| `repo_note.go` | `NoteRepo` — CRUD for notes, `ListForStudents` (date range) |
| `repo_report.go` | `ReportRepo` — CRUD for reports |
| `repo_voice_note.go` | `VoiceNoteRepo` — CRUD for voice_notes, `SetTranscript`, `MarkProcessed`, `MarkPurged`, `ListStale` |
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
| `extract.go` | `Extractor` interface + `llmExtractor`: both extraction passes, their schemas, and the pronoun guard |
| `notes.go` | `NoteCreator` interface + `dbNoteCreator`, note CRUD handlers |
| `report_generator.go` | `ReportGenerator` interface + `llmReportGenerator` (HTML output) |
| `report_prompt.go` | GPT prompt construction for report generation. `BuildReportPrompt` emits ranked sections: the Level's Report Specification (mandatory), then ad-hoc instructions (override the Specification where they conflict), then Student Notes (sole source of facts), then feedback. Requests HTML output. |
| `reports_handler.go` | POST /reports, POST /reports/{id}/regenerate, report CRUD handlers. Both generation endpoints pre-flight-resolve every selected student's Class → Level and refuse the whole request with `400` (naming the offending Levels) if any Level's `report_instructions` is trimmed-empty — no report row created, no LLM call made. |
| `audio_format.go` | Magic-byte detection, 3GP patching, filename extension fixing |
| `logger.go` | Dual stdout+Sentry structured logging via `log/slog`; `InitLogger()` wires `slog.NewMultiHandler` when `SENTRY_DSN` is set; request-scoped logger via context |
| `job_queue.go` | `Keyed` constraint, `JobQueue[T]` generic interface for async job queues |
| `job_queue_mem.go` | `MemQueue[T]` — generic in-memory `JobQueue` implementation with worker pool |
| `voice_note_job.go` | `VoiceNoteJob` type, job status constants, `NoteLink`, `JobPassage`, the `NoNotes*` reasons |
| `voice_note_process.go` | `processVoiceNote` pipeline (transcribe→extract→notes) |
| `voice_note_passages.go` | `assemblePassages`: extraction's passages → one note per child, and the card's passage list. Pure |
| `voice_note_cleanup.go` | Background goroutine to delete voice note rows, transcripts and any leftover audio after retention |
| `voice_note_jobs.go` | GET /voice-notes/jobs, POST /voice-notes/jobs/retry, POST /voice-notes/jobs/dismiss — voice note job list, retry, dismiss handlers |
| `voice_note_assemble.go` | POST /voice-notes/{uploadId}/assemble — run extraction's second pass against a class the teacher picked, and file its notes |
| `match.go` | `MatchStudent` — resolves one spoken label to a student in one class (exact, name part, then gated fuzzy). No live caller since #127: the model resolves names in pass 2, and the class picker runs that pass. Kept, with its corpus, for the passage-level student picker |
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

`apiError` struct (`errors_http.go`) carries HTTP status, machine-readable code, human message, and an optional `Details map[string]string` field for structured context (e.g. `conflictStudentName` on alias collision). All responses are JSON.

Handlers hand errors to one of two writers rather than formatting bodies themselves:

- `writeError(w, r, err)` — `*apiError` (bare or wrapped) is written by `writeAPIError` with its own status; `ErrNotFound` becomes a generic 404 `{"error":"not found"}`; anything else is an internal error. It is the one-liner for `userIDFromRequest` / `groupIDFromRequest` failures. Duplicate and in-use errors are *not* mapped: each 409 site builds a body the frontend reads by field, so those branches stay bespoke, as do 404s whose text names the entity (`"note not found"`, `"student Zephyrine not found"`).
- `writeInternalError(w, r, err)` — the only way to answer 500. The body is always `{"error":"internal server error"}`; the real `err` is logged at Error with `"op"` set to the calling handler's name (via `callerAt`, the sibling of `callerName`) and the method, never the URL path (client-controlled — see `requireStudentOwnership`). `err.Error()` must never reach a 500 body: repo and driver errors name tables, files and hosts. A handler that wants a more specific generic body (`"failed to load student"`) may still write it, but must log the cause itself.

Missing or invalid session is 401 (`code: unauthorized`) from both `userIDFromRequest` and `groupIDFromRequest`; a session with no active organisation is 403 (`no_active_org`), from the auth middleware and `groupIDFromRequest` alike. The frontend treats 401 and 403 identically (session expired).

Repo-level errors:
- `ErrNotFound` — entity not found
- `ErrDuplicate` — generic unique constraint violation (used by class/student/note repos)
- `*ErrDuplicateAlias` — alias-specific conflict that carries the canonical name of the student who owns the conflicting alias, so the handler can include it in the 409 `details` field
- `*ErrDuplicateStudentName` — returned by `StudentRepo.Move` when the student's canonical name collides with a name or alias already in the target class; wraps `ErrDuplicate` (`Unwrap`) so generic `errors.Is(err, ErrDuplicate)` checks still work, while `errors.As` recovers the conflicting name for the 409 `details.conflictStudentName` field
- `*ErrLevelInUse` — carries the count of Classes still referencing a Level being deleted, so `handleDeleteLevel` can return a 409 stating how many Classes must move first; the DB's `ON DELETE RESTRICT` on `classes.level_id` backs this up but can't name the count itself

## Observability / Sentry

`github.com/getsentry/sentry-go` v0.46.2. `InitSentry()` (`sentry.go`) reads `SENTRY_DSN` / `SENTRY_RELEASE` / `SENTRY_ENVIRONMENT` at startup — no-op if DSN is empty. `sentryhttp` middleware wraps the top-level handler in `main.go` (auto-captures panics; `Repanic: true`). Authenticated requests are tagged with the Clerk user ID. `BeforeSend` scrubs request bodies, query strings, cookies, auth headers, and name-shaped strings from exception values. `captureFeedback()` is available for non-error feedback events (task #19). DSN, release, and environment are baked into the Docker image via `VITE_SENTRY_DSN` / `VITE_APP_VERSION` / `VITE_SENTRY_ENVIRONMENT` build-args.

### LLM Provider Metrics

`instrumentProvider()` (`llm_provider.go`) wraps the `LLMProvider` returned by `LoadProvider()`
so `openaiProvider` and `mistralProvider` are both covered without either implementation
knowing about metrics. It uses `sentry.NewMeter(ctx)` — opt-out (`ClientOptions.DisableMetrics`,
never set by `InitSentry()`), so no config change was needed. `NewMeter` returns a noop meter
whenever no client is bound to the hub, which is why these metrics no-op cleanly when
`SENTRY_DSN` is unset in local dev (`TestRecordLLMCall_NoopWhenSentryUninitialised`).

Every `ChatJSON` / `ChatText` / `Vision` / `Transcribe` call emits, tagged with `task`
(`LLMTask`: `extraction` / `report` / `vision` / `transcription`), `model`
(`provider.Model(task)`) and `provider` (`provider.Name()`):

- `llm.call.duration` — Distribution, milliseconds, call latency.
- `llm.call.count` — Count, always emitted, one per call.
- `llm.call.errors` — Count, emitted only on failure, with an additional `kind` attribute:
  `deadline_exceeded` (`errors.Is(err, context.DeadlineExceeded)` — the 120s chat / 300s
  transcribe deadlines in `llm_provider.go`), `canceled` (`context.Canceled` — caller
  disconnects, job shutdown), or `other`. On the Mistral transcribe path specifically, the
  SDK's own `http.Client` timeout races the ctx deadline (both set to `llmTranscribeTimeout`,
  `llm_provider_mistral.go`); if the SDK timeout wins, its error does not satisfy
  `errors.Is(err, context.DeadlineExceeded)` and lands in `other` instead — pre-existing to how
  the SDK is wired, not introduced by this instrumentation.

These measure the provider boundary's operational health, not answer quality — the three
attributes this code sets are durations, counts, model IDs, and task names, never a prompt,
transcript, or student name. 

Production LLM quality signals live in `artifact_feedback` and the eval harness
(`backend/evals/`) instead.

### Structured Logs

`InitLogger()` (`logger.go`) must be called after `InitSentry()`. When `SENTRY_DSN` is set it builds a `slog.NewMultiHandler` combining the stdout handler with a `sentryslog` handler (`github.com/getsentry/sentry-go/slog`). All `log.Info/Warn/Error` call sites are unchanged. Default `sentryslog` behaviour: `Debug`/`Info`/`Warn` → Sentry structured log entry only; `Error`/`Fatal` → structured log entry **and** a Sentry event (Issue).

In `voice_note_process.go`, the two `process voice note: mention dropped` records and the
`process voice note completed` record carry `model` (`extractor.Model()`) and `prompt_hash`
(`ExtractionPromptHash`, `prompts_version.go`) — the same values stamped on the note row
(task #96).

The two-pass contract (#125) renamed what those records count, and any saved Sentry
query on the old names is now reading a field nothing writes. There is no confidence
score in the contract, so `dropped_low_confidence`, `mentions_total` and
`mentions_below_0_7` are gone. The drop reasons are now `unattributed` (a passage about
one child that reached none of them) and `no_roster_match` (a child the roster lookup
could not find). The completion record carries `passages_total` with a per-kind
breakdown — `passages_child`, `passages_unknown`, `passages_group`, `passages_none` —
plus `dropped_unattributed` and `dropped_no_roster_match`. The per-kind counts are what
separate a prompt regression (every block `unknown`) from a quiet recording.

The class picker is the second place pass 2 runs, and the one where it works against a
class a human chose, so `assemble notes` carries the same breakdown. Its per-note record
keeps the pinned query string `process voice note: passage recovered` with
`route=class_picker`.

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

### Assertions must be able to fail

A 2026-08 mutation audit (task #92) found ~17 assertions across telemetry, reports, jobs and
students that passed while checking less than their name claimed — every one the same idiom:
assert the envelope, skip the content. Three rules, each with an in-tree template:

1. **Assert by value, not by shape.** Decoding a field and never comparing it (`resp.HTML`
   read, not asserted), or checking a log line for `"upload_id"` rather than
   `"upload_id":`+the real id, cannot fail when the field is wired to the wrong source.
   Template: `TestHandleRegenerateReport_ResponseShape` (`reports_regen_test.go`) — every
   decoded field compared, including the regenerated body vs. the stored one.
2. **Make expected values distinct.** Three buckets that each expect length 1 stay green
   when two of them are swapped. Size fixtures so no two counters can be confused —
   `TestProcessJob_CompletionRecordCountsMentions` (7/2/1/4/3) and `TestJobList_GroupsByStatus`
   (3/2/1, asserted by upload id) — and prefer asserting the members over the count.
3. **Pair every absence with a presence.** `NotContains` on a string the code never emits,
   or on a fixture that never held the key (`sentry_test.go`'s `Cookie`), proves nothing.
   Put the thing in the fixture so the code has to remove it, and assert the positive arm
   in the same test: `TestBuildReportPrompt_InstructionsSectionOnlyWhenGiven`,
   `students_test.go`'s `map[string]any` + key-presence check for an omitted field.

When in doubt, apply the named mutation (swap two fields, blank a value, delete the guard) and
confirm the suite fails; a fix without that evidence is not a fix.

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
| `UPLOAD_RETENTION_HOURS` | No | Hours to keep a voice note's row, its transcript, and any audio still on disk, counted from processing or dismissal, or from upload if neither happened (default 168 = 7 days) |
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
  scoring/assemble.js           folds pass-2 passages into per-child notes before scoring
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

`make eval` builds `bin/eval-cli` (from `cmd/eval-cli/`), then invokes promptfoo. For each test case, promptfoo calls eval-cli as an **exec-prompt function** (passing a single JSON arg), and eval-cli returns a messages array — no LLM call. Promptfoo sends the messages to its native provider (with structured output schema for extraction), folds an extraction response into per-child notes with `scoring/assemble.js`, scores the result, and writes it out. Model selection lives entirely in the config files; `EVAL_MODEL` is no longer read.

Extraction rows grade **pass 2 only** — promptfoo makes one call per test, so each fixture names the class pass 1 is taken to have pinned, in `vars.class_name`. See `backend/evals/README.md`, "What extraction grades".

Debug a single case:
```bash
cd backend
make bin/eval-cli
./bin/eval-cli '{"vars":{"transcript":"Alice read well.","class_name":"Grade 3A","classes":[{"name":"Grade 3A","students":[{"name":"Alice Chen"}]}]},"config":{"task":"build-extract-prompt"}}'
```

### Eval CLI (`cmd/eval-cli`)

| Config task | Reads from `vars` | Builds |
|---|---|---|
| `build-extract-prompt` | `transcript`, `classes`, `class_name` | `BuildPassagePrompt` for the named class → messages array (system + user) |
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
