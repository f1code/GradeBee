# Backend Architecture

## Overview

Go HTTP backend for GradeBee, a teacher tool for managing student rosters and processing audio recordings (upload → transcribe). Deployed as a **Scaleway serverless function** (single handler entrypoint). Local dev via `cmd/server/main.go`.

**Package:** `handler` (all source files in `backend/` share this package).

## Entrypoint & Routing

**`handler.go`** — exports `Handle(w, r)`, the single HTTP handler.

| Method | Path         | Auth | Handler              | Description                              |
|--------|-------------|------|----------------------|------------------------------------------|
| GET    | `/` `/health` | No  | inline               | Health check                             |
| POST   | `/setup`    | Yes  | `handleSetup`        | Provision Drive workspace                |
| GET    | `/students` | Yes  | `handleGetStudents`  | Read roster from Sheets                  |
| POST   | `/upload`   | Yes  | `handleUpload`       | Upload audio to Drive                    |
| POST   | `/transcribe`| Yes | `handleTranscribe`   | Download from Drive → Whisper → text     |
| POST   | `/extract`  | Yes  | `handleExtract`      | Analyze transcript → matched students    |
| POST   | `/notes`    | Yes  | `handleCreateNotes`  | Create Google Doc notes for students     |

Auth is Clerk JWT via `clerkhttp.RequireHeaderAuthorization()` middleware. CORS handled inline.

## Dependency Injection

**`deps.go`** — defines `deps` interface + `prodDeps` implementation + package-level `serviceDeps` variable.

```
deps interface {
    GoogleServices(r) → *googleServices
    GetTranscriber()  → Transcriber
    GetRoster(ctx, svc) → Roster
    GetDriveStore(svc)  → DriveStore
}
```

Tests override `serviceDeps` with stubs. All handler functions call through this interface, never instantiate services directly.

### Key Interfaces

| Interface     | File             | Prod Implementation     | Purpose                        |
|--------------|------------------|------------------------|--------------------------------|
| `deps`       | `deps.go`        | `prodDeps`             | Top-level DI container         |
| `Roster`     | `deps.go`        | `sheetsRoster`         | Read student data from Sheets  |
| `Transcriber`| `deps.go`        | `whisperTranscriber`   | Audio→text via OpenAI Whisper  |
| `DriveStore` | `drive_store.go` | `sheetsDriveStore`     | Upload/download files on Drive |
| `Extractor`  | `extract.go`     | `gptExtractor`         | Transcript→student extraction  |
| `NoteCreator`| `notes.go`       | `driveNoteCreator`     | Create Google Doc notes        |

## External Services

### Google APIs (`google.go`)
- **Auth flow:** Clerk JWT → extract user ID → `getGoogleOAuthToken` (Clerk Backend API) → OAuth2 token → Drive/Sheets/Docs clients.
- Scope: `drive.file` only (app can only access files it created).
- `googleServices` struct holds `*drive.Service`, `*sheets.Service`, `*docs.Service`, `*clerkUser`.

### Clerk (`auth.go`, `clerk_metadata.go`)
- JWT verification via middleware.
- OAuth token retrieval: `user.ListOAuthAccessTokens` for `oauth_google`.
- **Private metadata** stores Drive resource IDs (`gradeBeeMetadata` struct: folder, spreadsheet, uploads/notes/reports subfolder IDs). This avoids needing `drive.readonly` scope to find resources.

### OpenAI Whisper (`deps.go`)
- `whisperTranscriber` uses `go-openai` client.
- Handles audio format detection and 3GP→MP4 patching (`audio_format.go`).

## File-by-File Reference

| File                | Responsibility                                                    |
|---------------------|------------------------------------------------------------------|
| `cmd/server/main.go`| Local dev server; loads `.env`, inits Clerk, starts HTTP          |
| `handler.go`        | Routing, CORS, request logging, `Handle` entrypoint              |
| `deps.go`           | DI interface, prod implementations, `serviceDeps` variable        |
| `google.go`         | Google API client construction, `apiError` type, `createFolder`   |
| `auth.go`           | `getGoogleOAuthToken` — Clerk → Google OAuth token               |
| `clerk_metadata.go` | Read/write `gradeBeeMetadata` in Clerk user private metadata      |
| `setup.go`          | POST /setup — create Drive folder tree + ClassSetup spreadsheet   |
| `students.go`       | GET /students — read & parse roster, `parseStudentRows`           |
| `roster.go`         | `sheetsRoster` — Roster interface impl backed by Sheets API       |
| `upload.go`         | POST /upload — multipart audio → Drive uploads folder             |
| `transcribe.go`     | POST /transcribe — Drive download → Whisper API                   |
| `drive_store.go`    | `DriveStore` interface + `sheetsDriveStore` (Drive CRUD)          |
| `extract.go`        | `Extractor` interface + GPT implementation for transcript analysis|
| `extract_handler.go`| POST /extract — transcript analysis → matched students            |
| `notes.go`          | `NoteCreator` interface + Drive/Docs implementation               |
| `notes_handler.go`  | POST /notes — create Google Doc notes for confirmed students      |
| `audio_format.go`   | Magic-byte detection, 3GP patching, filename extension fixing     |
| `logger.go`         | slog-based structured logging, request-scoped via context         |

## Drive Folder Structure (per user)

```
GradeBee/              ← root folder (ID in metadata.FolderID)
├── uploads/           ← audio files (metadata.UploadsID)
├── notes/             ← (metadata.NotesID)
├── reports/           ← (metadata.ReportsID)
└── ClassSetup         ← Google Sheet (metadata.SpreadsheetID)
    └── Sheet "Students": columns A=class, B=student (header row 1)
```

## Error Handling

`apiError` struct (`google.go`) carries HTTP status, machine-readable code, and human message. Handlers check `errors.As(err, &apiError)` and call `writeAPIError`. All responses are JSON.

## Testing

- Tests in `*_test.go` files override `serviceDeps` with stubs.
- `testutil_test.go` has shared test helpers.
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
