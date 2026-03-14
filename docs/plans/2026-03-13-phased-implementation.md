# GradeBee — Phased Implementation Plan

## Stack

- **Frontend**: React SPA, static deploy to Scaleway Object Storage (+ CDN)
- **Backend**: Go serverless functions on Scaleway
- **Auth**: Clerk (Google OAuth with custom scopes for Drive/Sheets)
- **Storage**: Google Drive (user's own Drive — no DB needed initially)
- **AI**: OpenAI Whisper API (transcription), Claude API (extraction/summarization)

## Key Design Decisions

- Clerk manages auth AND stores Google OAuth tokens (access + refresh). Retrieve via Clerk Backend API. Requires custom Google OAuth app in Clerk config to request `drive.file` + `spreadsheets.readonly` scopes. **No `drive.readonly`** — full Drive access is a security no-go; scopes cannot be restricted to the app folder.
- **Upload strategy**: Phase 1–5 use web app upload (drag-drop or file picker). Phase 6 adds "Add from Drive" via Google Picker — user selects files from their Drive (e.g. recent uploads); Picker grants per-file access under `drive.file` without broader scope.
- Notes stored as Google Docs (not raw Markdown) to enable inline teacher feedback.
- A lightweight metadata index (JSON file per student in Drive) avoids re-parsing all docs for report generation.
- No database. Clerk = user store, Google Drive = data store, metadata index = query layer.

---

## Phase 1: Auth & Google Drive Connection

**Goal**: User signs in, connects Google Drive, system creates folder structure.

### Tasks
1. Scaffold React app (Vite + React + Clerk provider)
2. Scaffold Go serverless function project for Scaleway
3. Configure Clerk with Google OAuth (custom app, Drive/Sheets scopes)
4. Frontend: sign-in flow, post-auth redirect
5. Backend function: `POST /setup` — uses Clerk token to get Google OAuth tokens, creates `GradeBee/uploads/`, `GradeBee/notes/`, `GradeBee/reports/` in user's Drive
6. Frontend: show connection status + folder link
7. Deploy static site to Scaleway bucket, deploy function

### Verify
- User signs in with Google via Clerk
- Backend retrieves Google tokens from Clerk
- Folder structure appears in user's Drive

---

## Phase 2: Student List

**Goal**: Read class/student data from user's Google Sheets spreadsheet.

### Tasks
1. Backend function: `GET /students` — reads `ClassSetup` spreadsheet via Sheets API
2. Frontend: display student list grouped by class
3. Handle missing/malformed spreadsheet gracefully (prompt user to create one from template)

### Verify
- Spreadsheet with `class | student` columns is read correctly
- Students display grouped by class in UI

---

## Phase 3: Voice Upload & Transcription

**Goal**: Teacher uploads audio, system transcribes it.

### Tasks
1. Frontend: upload UI (drag-and-drop or file picker)
2. Backend function: `POST /upload` — saves audio to `GradeBee/uploads/` in Drive
3. Backend function: `POST /process` (or triggered by upload) — downloads audio from Drive, sends to Whisper API, returns transcript
4. Store raw transcript temporarily (in-memory or function response)

### Verify
- Audio file appears in Drive `uploads/`
- Transcript is returned and displayed to user

---

## Phase 4: Note Generation

**Goal**: Extract student names from transcript, match to student list, generate structured notes as Google Docs.

### Tasks
1. Backend: Claude API call to extract student name(s) + topic from transcript
2. Backend: fuzzy match extracted names against student list (handle confidence threshold — if low, return candidates for manual selection)
3. Frontend: confirmation UI — show matched student, topic, summary. Allow correction.
4. Backend: `POST /notes` — create Google Doc in `GradeBee/notes/{class}_{student}/` with structured content (transcript, summary, date, topic)
5. Backend: update metadata index (`GradeBee/notes/{class}_{student}/index.json`) with note entry (date, topic, summary, doc ID)
6. Include feedback section in Google Doc (editable by teacher)

### Verify
- Transcript → student match works (including fuzzy)
- Google Doc created with correct structure
- Metadata index updated
- Teacher can edit feedback section in Doc

---

## Phase 5: Report Card Generation

**Goal**: Aggregate notes per student into a report card.

### Tasks
1. Backend: `POST /reports` — read metadata index for student, fetch summaries (not full docs), call Claude to generate report narrative
2. Backend: create report as Google Doc in `GradeBee/reports/{term}/`
3. Frontend: report generation UI — select term/date range, select students or class
4. Include feedback section in report Doc
5. Backend: `POST /reports/regenerate` — re-read feedback from Doc, regenerate with feedback incorporated (only touches the one report, reads index + feedback, not all note docs)

### Verify
- Report aggregates notes correctly
- Report quality is coherent across multiple notes
- Feedback → regenerate cycle works without re-reading all note documents

---

## Phase 6: Add from Drive (Optional)

**Goal**: Reduce friction — let users pick audio files from their Drive (e.g. files they uploaded elsewhere) without re-uploading.

### Tasks
1. Frontend: "Add from Drive" button — opens Google Picker (filtered for audio files)
2. User selects file(s) from Drive; Picker grants per-file access under `drive.file` (no additional scopes)
3. Backend: receive file ID(s), copy or reference file in `GradeBee/uploads/`, trigger processing pipeline
4. Notification to teacher (email or in-app) when note is ready for review
5. Error handling, retry logic, rate limiting

### Verify
- User uploads audio to Drive elsewhere → opens GradeBee → "Add from Drive" → selects file → note generated without re-upload

---

## Deployment

| Component | Target |
|-----------|--------|
| React SPA | Scaleway Object Storage bucket + Scaleway CDN |
| Go functions | Scaleway Serverless Functions |
| DNS/TLS | Scaleway Domains or external DNS pointing to CDN |
| Secrets | Scaleway Secret Manager (Clerk keys, Google client secret, AI API keys) |
