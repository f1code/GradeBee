# Backend Architecture

What lives where. Reasoning lives in code comments and `docs/adr/`; domain terms in `CONTEXT.md`.

## Overview

Go HTTP backend for GradeBee: student rosters, voice/text notes (upload → transcribe → extract), report cards. One package, `handler`, in `backend/`.

**Deployment:** the Go binary embeds the React `dist/` (`static.go`, `embed.FS`) and serves it. Dokku's nginx terminates TLS and gzips. See `Dockerfile`.

**Storage:** SQLite (`modernc.org/sqlite`, WAL). Audio on local disk until transcribed.

## Entrypoint & Routing

- `handler.go` — `Handle`: request logger and `X-Request-ID`, `/health`, CORS (answers `OPTIONS`), SPA fallthrough for non-`/api/` paths, then `apiMux`.
- `router.go` — `newAPIMux(auth)`: Go 1.22 `ServeMux` method+pattern routes, each wrapped in `auth` (`clerkAuthMiddleware` in prod, `fakeAuth` in tests). `idParam(r, "id")` reads wildcards. An `/api/` catch-all answers JSON 404, wrong methods included.
- `static.go` — `spaHandler()`: `/assets/*` immutable for a year; `index.html` and `/manifest.json` `no-cache`.

| Method | Path | Auth | Handler | Description |
|--------|------|------|---------|-------------|
| GET | `/` `/health` | No | inline | Health check |
| GET | `/api/classes` | Yes | `handleListClasses` | List user's classes with student counts |
| POST | `/api/classes` | Yes | `handleCreateClass` | Create a class (`{levelId, day, timeSlot}`) |
| PUT | `/api/classes/{id}` | Yes | `handleUpdateClass` | Update a class (`{levelId, day, timeSlot}`) |
| DELETE | `/api/classes/{id}` | Yes | `handleDeleteClass` | Delete class + cascade |
| GET | `/api/classes/{id}/students` | Yes | `handleListStudents` | List students in a class |
| POST | `/api/classes/{id}/students` | Yes | `handleCreateStudent` | Add a student |
| GET | `/api/students` | Yes | `handleGetStudents` | Full roster grouped by class |
| PUT | `/api/students/{id}` | Yes | `handleUpdateStudent` | Rename / move (`{classId}`). A move drops colliding aliases (`droppedAliases`); a name collision is 409 `student_name_conflict` |
| DELETE | `/api/students/{id}` | Yes | `handleDeleteStudent` | Delete student + cascade |
| GET | `/api/students/{id}/notes` | Yes | `handleListNotes` | List notes for a student |
| POST | `/api/students/{id}/notes` | Yes | `handleCreateNote` | Create a manual note |
| GET | `/api/students/{id}/aliases` | Yes | `handleListAliases` | List aliases |
| POST | `/api/students/{id}/aliases` | Yes | `handleAddAlias` | Add an alias |
| DELETE | `/api/students/{id}/aliases/{aliasId}` | Yes | `handleRemoveAlias` | Remove an alias |
| GET | `/api/notes/{id}` | Yes | `handleGetNote` | Get single note |
| PUT | `/api/notes/{id}` | Yes | `handleUpdateNote` | Edit note summary |
| DELETE | `/api/notes/{id}` | Yes | `handleDeleteNote` | Delete a note |
| POST | `/api/reports` | Yes | `handleGenerateReports` | Generate report cards (HTML) |
| POST | `/api/reports/{id}/regenerate` | Yes | `handleRegenerateReport` | Regenerate with feedback |
| GET | `/api/students/{id}/reports` | Yes | `handleListReports` | List reports for a student |
| GET | `/api/reports/{id}` | Yes | `handleGetReport` | Get single report HTML |
| DELETE | `/api/reports/{id}` | Yes | `handleDeleteReport` | Delete a report |
| POST | `/api/feedback` | Yes | `handleSubmitFeedback` | Thumbs rating on a report or auto note |
| POST | `/api/voice-notes/upload` | Yes | `handleUpload` | Upload audio to disk + dispatch job |
| POST | `/api/text-notes/upload` | Yes | `handleTextNotesUpload` | Pasted text + dispatch extraction job |
| POST | `/api/voice-notes/drive-import` | Yes | `handleDriveImport` | Download from Drive + dispatch job |
| GET | `/api/google-token` | Yes | `handleGoogleToken` | Google OAuth token for Drive Picker |
| GET | `/api/voice-notes/jobs` | Yes | `handleJobList` | List user's upload jobs |
| POST | `/api/voice-notes/jobs/retry` | Yes | `handleJobRetry` | Retry failed jobs |
| POST | `/api/voice-notes/jobs/dismiss` | Yes | `handleJobDismiss` | Dismiss finished jobs |
| POST | `/api/voice-notes/{uploadId}/assemble` | Yes | `handleAssembleNotes` | Rerun pass 2 against a class the teacher picked (`{className}`) and file its notes |
| POST | `/api/voice-notes/{uploadId}/assign` | Yes | `handleAssignPassages` | File unattributed passages to one child (`{classId, studentId, passages: [{kind, summary, spokenLabels?}], appendToNoteId?}`) |
| DELETE | `/api/voice-notes/{uploadId}/assign/{studentId}` | Yes | `handleUndoAssignment` | Delete the child's `assigned` notes from this recording; returns `{noteIds}` |
| GET | `/api/levels` | Yes | `handleListLevels` | List the Group's Levels |
| POST | `/api/levels` | Admin | `handleCreateLevel` | Create a Level (`{name}`) |
| PUT | `/api/levels/{id}` | Admin | `handleUpdateLevel` | Rename / set Report Instructions |
| DELETE | `/api/levels/{id}` | Admin | `handleDeleteLevel` | Delete a Level; 409 with the count while Classes use it |

## Async Upload Pipeline

Uploads (`voice_note_upload.go`, `voice_note_drive_import.go`, text notes) save the input, create a `voice_notes` row (minting `trace_id`), and publish a `VoiceNoteJob` to `MemQueue` (`job_queue_mem.go`, 4 workers, started in `cmd/server/main.go`). `processVoiceNote` (`voice_note_process.go`):

```
queued
  ├─ transcribe   provider (Voxtral/Whisper), class names as context bias;
  │               delete audio, set purged_at, write voice_notes.transcript
  ├─ extract      pass 1: class list → the recording's class, or "" (decline:
  │                 job done, no notes, reason class_unclear)
  │               pass 2: transcript (header cut) vs that class's roster
  │                 → passages {kind, spoken_labels, student, summary}
  │               pronoun-only child passages demoted to unknown
  ├─ create notes assemblePassages (voice_note_passages.go):
  │                 child/absent + student → that child's note
  │                 no student, unknown    → nobody; stays on the card
  │                 group → whole roster minus absent; nobody when names
  │                   were spoken and none resolved (ADR 0004)
  │                 none  → dropped
  │               fileNotes: one transaction, stamped with trace_id
  └─ done
```

A failure sets `failed`; retry republishes the same job (same `trace_id`, transcription skipped if done). Jobs live in memory, keyed `userId/<uploadId>`, and are gone on restart; the `voice_notes` row holds `trace_id` and the transcript.

### Class picker (`voice_note_assemble.go`)

A done job with no notes carries a reason (`noNotesReason`) and `canPickClass`. The card offers the picker on `canPickClass` alone (`class_unclear`, `no_name_matched`). The endpoint reruns pass 2 against the picked class and folds it with `assemblePassages`; notes are `source = reviewed`. It shares the per-upload in-process lock (`takeUploadLock`) with assign — single-instance only.

### Filing by hand (`voice_note_assign.go`, `voice_note_unassign.go`)

The done card (`PassageReview.tsx`) lists unattributed passages. Assign files the ticked rows plus the card's group passages to one child as a `source = assigned` note dated from the recording, or appends them to the note the card holds for that child (`appendToNoteId`). Before filing, each row's spoken labels and any `STUDENT`/`NAME` placeholder become the child's roster name (`useRosterName`). A replay finds the earlier note by text and writes nothing. Undo deletes the child's `assigned` notes from the recording (by `trace_id`) and drops their links from the job.

### Cleanup (`voice_note_cleanup.go`)

Audio is deleted right after transcription. The row and its transcript go after `UPLOAD_RETENTION_HOURS` (default 168), counted from `processed_at`, or `created_at` if never processed.

## Dependency Injection

`deps.go`: `deps` interface, `prodDeps`, package-level `serviceDeps`. Handlers call through it; tests swap it for stubs.

| Interface | File | Prod | Purpose |
|-----------|------|------|---------|
| `Roster` | `roster.go` | `dbRoster` | Read students from DB |
| `Transcriber` | `transcriber.go` | `providerTranscriber` | Audio → text via `LLMProvider` |
| `Extractor` | `extract.go` | `llmExtractor` | Transcript → passages (two passes); `ExtractPassages` runs pass 2 alone |
| `NoteCreator` | `notes.go` | `dbNoteCreator` | `CreateNotes` files a batch in one transaction |
| `ReportGenerator` | `report_generator.go` | `llmReportGenerator` | Report cards (HTML) |
| `JobQueue[T]` | `job_queue.go` | `MemQueue[T]` | In-memory job queue |

## External Services

### Clerk (`auth.go`, `handler.go`)

- `clerkAuthMiddleware` verifies the JWT and rejects a session without an active Organization (`403 no_active_org`).
- `userIDFromRequest`, `groupIDFromRequest` (active Org = Group), `isAdmin` (`org:admin`).
- Levels are Group-owned: `LevelRepo` scopes every method by `group_id`; writes need `isAdmin`. `ClassRepo` checks `level_id` belongs to the caller's Group.
- Google OAuth token for the Drive Picker: `user.ListOAuthAccessTokens` (`oauth_google`).

### LLM Provider (`llm_provider*.go`)

`LLMProvider` interface; `openaiProvider` (OpenAI, and OpenRouter at `OPENROUTER_BASE_URL`, no transcription) and `mistralProvider` (chat + Voxtral). `LoadProvider(db)` picks one per task from `LLM_PROVIDER_EXTRACTION`/`_REPORT`/`_TRANSCRIPTION` (default `LLM_PROVIDER`, default `mistral`) and fails fast on a bad name or missing key. Production: `mistral`, reports on `openrouter` (ADR 0006). Models: `LLM_MODEL_*`.

`instrumentedProvider` writes one `llm_calls` row per answered call (caller from `withLLMCaller`) and emits Sentry metrics.

## Database

SQLite, WAL (`db.go`). Migrations in `sql/`, embedded, applied in filename order (`migrate.go`). One `Repo*` per table in `repo_*.go`.

| Table | Purpose |
|-------|---------|
| `levels` | Group-owned curriculum tiers; `name` unique per `group_id`; blank `report_instructions` blocks report generation (400) |
| `classes` | A Level instance: `level_id` (`RESTRICT`), `day` (`CHECK` weekday), optional `time_slot`. Name derived in SQL, never stored. Unique `(user_id, level_id, day, time_slot)` |
| `students` | Students per class |
| `student_aliases` | Name variants, unique per class, case-insensitive |
| `notes` | Per-student notes. `trace_id` names the source recording (no FK). `source`: `auto`, `reviewed` (model-written; edit/delete fires implicit thumbs-down), `assigned`, `manual` (not model-written) |
| `reports` | Generated HTML reports |
| `voice_notes` | Upload tracking: file path, `processed_at`, `purged_at`, `transcript`, `trace_id` (unique UUID; row ids get reused) |
| `llm_calls` | One row per answered AI call: user, trace, provider, model, task, tokens/audio seconds, `ok`. Read by `scripts/ai_cost.sql`; see `docs/ai-usage.md` |
| `artifact_feedback` | Append-only thumbs ratings: `explicit`, `regenerated`, `edited`, `deleted` |

## Authorization

Every endpoint checks ownership: classes by `class.UserID`; students, notes and reports through `requireStudentOwnership(w, r, studentID, userID, notFoundMsg)`, which writes the 404 itself:

```go
if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
    return
}
```

## Error Handling (`errors_http.go`)

- `writeError(w, r, err)` — `*apiError` with its own status; `ErrNotFound` → 404; else 500.
- `writeInternalError(w, r, err)` — the only way to answer 500. Body is always `{"error":"internal server error"}`; `err` is logged, never returned.
- 401 for a missing session, 403 `no_active_org` without an active Org. 409 bodies are built per site.
- Repo errors: `repo_errors.go`.

## Observability

- Sentry (`sentry.go`): no-op without `SENTRY_DSN`; panics captured; `BeforeSend` scrubs bodies, headers and name-shaped strings.
- Logs (`logger.go`): `slog` to stdout and Sentry; Error also raises an Issue. Call `InitLogger()` after `InitSentry()`.
- LLM metrics: `llm.call.duration`, `llm.call.count`, `llm.call.errors` (`kind`), tagged `task`, `model`, `provider`.
- Pipeline records carry `trace_id`, `model`, `prompt_hash` and per-kind passage counts. `process voice note: passage recovered` covers every route a passage reaches a child (`route` = `class_picker` | `manual`); `assignment undone` is its reverse.
- **No student names in logs or errors** ([ADR 0003](../docs/adr/0003-no-child-pii-in-telemetry.md)). Log `student_id`; new telemetry on a student path needs a test asserting the name is absent.

## Prompt and model versioning (`prompts_version.go`)

Reports and model-written notes carry `model_version` and `prompt_hash`. `ExtractionPromptHash` covers both passes' templates and schema bytes; `ReportPromptHash` the report templates. Bump `PromptVersionTag` for logic changes outside templates and schemas.

## Type Generation

tygo writes `frontend/src/api-types.gen.ts` from Go `json` structs (`tygo.yaml`). After changing one: `cd backend && make generate`, commit the file. `make check-types` runs in `make test`.

## Testing

Tests swap `serviceDeps` for stubs (`testutil_test.go`); `setupTestDB(t)` gives an in-memory migrated DB. Run `make test` / `make lint` from the root.

Assertions must be able to fail:

1. **Assert by value, not shape.** Compare every decoded field (`TestHandleRegenerateReport_ResponseShape`).
2. **Make expected values distinct**, so swapped counters fail (`TestProcessJob_CompletionRecordCountsMentions`).
3. **Pair every absence with a presence**: put the thing in the fixture so the code must remove it (`TestBuildReportPrompt_InstructionsSectionOnlyWhenGiven`).

When unsure, apply the mutation and confirm the suite fails.

## Environment Variables

| Variable | Required | Purpose |
|----------|----------|---------|
| `CLERK_SECRET_KEY` | Yes | Clerk Backend API key |
| `LLM_PROVIDER` | No | `mistral` (default), `openai` or `openrouter` |
| `LLM_PROVIDER_EXTRACTION` / `_REPORT` / `_TRANSCRIPTION` | No | Per-task override; no `openrouter` for transcription |
| `OPENAI_API_KEY` / `MISTRAL_API_KEY` / `OPENROUTER_API_KEY` | When used | Provider keys |
| `OPENROUTER_BASE_URL` | No | Default `https://eu.openrouter.ai/api/v1` |
| `LLM_MODEL_EXTRACTION` / `_REPORT` / `_TRANSCRIPTION` | No | Model IDs (provider defaults) |
| `DB_PATH` | No | Default `/data/gradebee.db` |
| `UPLOADS_DIR` | No | Default `/data/uploads` |
| `UPLOAD_RETENTION_HOURS` | No | Default 168 |
| `ALLOWED_ORIGIN` | No | CORS origin (default `*`) |
| `PORT` | No | Default `8080` |
| `LOG_LEVEL` | No | DEBUG/INFO/WARN/ERROR/off |
| `SENTRY_DSN` / `SENTRY_RELEASE` / `SENTRY_ENVIRONMENT` | No | Baked in via `VITE_*` build-args |

## LLM Evaluation Harness

On-demand regression tests for extraction and reports (`backend/evals/`). `make eval` diffs against local baselines in `data/`; `make eval-baseline` re-pins them. Everything else: `backend/evals/README.md`.
