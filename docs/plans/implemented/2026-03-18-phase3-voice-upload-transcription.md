# Phase 3: Voice Upload & Transcription — Implementation Plan

## Goal

Teacher uploads audio via the web app, system saves it to Google Drive `GradeBee/uploads/` and transcribes it via OpenAI Whisper API, returning the transcript to the UI.

---

## Backend Changes

### 1. Add OpenAI Whisper dependency

- **File**: `backend/go.mod` — add `github.com/sashabaranov/go-openai` (popular Go OpenAI client)
- **File**: `backend/deps.go` — extend `deps` interface with `TranscriptionService` method (returns a transcriber interface) for testability
- **Env var**: `OPENAI_API_KEY`

### 2. `POST /upload` endpoint

- **File**: `backend/upload.go` (new)
- Parses `multipart/form-data` with a single `file` field
- Max file size: 25MB (Whisper API limit)
- Validates MIME type (allow `audio/*`, `video/webm` for browser recordings)
- Retrieves `UploadsID` from Clerk metadata (`getGradeBeeMetadata`)
- Uploads file to Google Drive `GradeBee/uploads/` folder via Drive API `Files.Create` with content
- Returns JSON: `{ "fileId": "<drive_file_id>", "fileName": "<name>" }`

### 3. `POST /transcribe` endpoint

- **File**: `backend/transcribe.go` (new)
- Accepts JSON body: `{ "fileId": "<drive_file_id>" }`
- Downloads audio from Drive via `Files.Get(...).Download()` (works under `drive.file` since we created the file)
- Streams audio bytes to OpenAI Whisper API (`whisper-1` model)
- Returns JSON: `{ "fileId": "...", "transcript": "...", "durationSeconds": N }`
- Error cases: file not found, file too large, Whisper API failure, rate limit

### 4. Wire routes in `handler.go`

- **File**: `backend/handler.go`
- Add `uploadHandler` and `transcribeHandler` vars (wrapped with `RequireHeaderAuthorization`)
- Add route cases: `path == "upload" && POST`, `path == "transcribe" && POST`
- Update CORS `Access-Control-Allow-Methods` to include existing methods (already has POST)

### 5. Tests

- **File**: `backend/upload_test.go` (new) — stub Drive service, test multipart parsing, size/type validation
- **File**: `backend/transcribe_test.go` (new) — stub Drive download + Whisper API, test happy path + errors
- Extend `deps` interface with `Transcriber` so tests can inject a fake

### 6. Deps interface extension

- **File**: `backend/deps.go`
  - Add `Transcriber` interface:
    ```go
    type Transcriber interface {
        Transcribe(ctx context.Context, filename string, audio io.Reader) (string, error)
    }
    ```
  - Add `Transcriber() Transcriber` to `deps` interface
  - `prodDeps` implementation creates OpenAI client from `OPENAI_API_KEY` env var

---

## Frontend Changes

### 7. Upload component

- **File**: `frontend/src/components/AudioUpload.tsx` (new)
- Drag-and-drop zone + file picker button (accept `audio/*`)
- Shows file name, size, upload progress
- On submit: `POST /upload` with `multipart/form-data`, auth header from Clerk
- On upload success: automatically calls `POST /transcribe` with returned `fileId`
- States: idle → uploading → transcribing → done (show transcript) / error
- Display transcript in a readable text area (read-only for now; Phase 4 will add student matching)

### 8. Wire into App

- **File**: `frontend/src/App.tsx`
- After setup is done and students are loaded, show `AudioUpload` component
- Simple layout: student list on left/top, upload area prominent

### 9. API helper

- **File**: `frontend/src/api.ts` (new or extend existing)
- `uploadAudio(file: File): Promise<{ fileId: string }>` 
- `transcribeAudio(fileId: string): Promise<{ transcript: string }>`
- Both include Clerk auth token in `Authorization: Bearer <token>` header

---

## Open Questions

1. **Combine upload+transcribe into one call?** Two separate endpoints give more control (upload first, transcribe later; retry transcription without re-upload). Recommend keeping separate.
%% separate 
2. **Browser audio recording?** Out of scope for Phase 3 — keep to file upload only. Could add MediaRecorder in a later phase.
%% later
3. **Whisper model choice?** `whisper-1` is the only option currently. If multilingual support needed, it handles it natively.
4. **File size limit?** Whisper API caps at 25MB. We should enforce this on both frontend (pre-upload check) and backend (multipart max size).
5. **Audio format?** Whisper supports mp3, mp4, mpeg, mpga, m4a, wav, webm. Validate on backend, show accepted formats on frontend.

---

## File Summary

| File | Action |
|------|--------|
| `backend/upload.go` | New — upload handler |
| `backend/transcribe.go` | New — transcribe handler |
| `backend/deps.go` | Edit — add `Transcriber` interface |
| `backend/handler.go` | Edit — wire new routes |
| `backend/upload_test.go` | New — upload tests |
| `backend/transcribe_test.go` | New — transcribe tests |
| `backend/go.mod` | Edit — add OpenAI dependency |
| `frontend/src/components/AudioUpload.tsx` | New — upload UI component |
| `frontend/src/App.tsx` | Edit — integrate AudioUpload |
| `frontend/src/api.ts` | New/Edit — API helpers |
