# Phase 4: Note Generation

## Goal

Extract student names from a transcript, match them to the roster, let the teacher confirm/correct, then save one structured note per student as a Google Doc.

## Current State

- **Phase 3 complete**: Upload → Whisper transcription works end-to-end. Frontend shows transcript in a read-only textarea with an "Upload another" button.
- **Backend interfaces exist**: `DriveStore` (upload/download), `Roster` (students/class names), `Transcriber`, `deps` DI pattern.
- **Clerk metadata** stores Drive folder IDs: `FolderID`, `UploadsID`, `NotesID`, `ReportsID`, `SpreadsheetID`.

## Overview of Changes

### 1. Backend: Extraction service (`backend/extract.go`)

New file. Defines the `Extractor` interface and OpenAI-backed implementation.

```go
// Extractor takes a transcript + student roster and returns structured extraction.
type Extractor interface {
    Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error)
}

type ExtractRequest struct {
    Transcript string
    Classes    []classGroup // from roster
}

type ExtractResponse struct {
    Students []MatchedStudent `json:"students"`
    Topic    string           `json:"topic"`
    Date     string           `json:"date"` // extracted or today
}

type MatchedStudent struct {
    Name       string  `json:"name"`
    Class      string  `json:"class"`
    Summary    string  `json:"summary"`    // per-student summary
    Confidence float64 `json:"confidence"` // 0.0–1.0
    Candidates []StudentCandidate `json:"candidates,omitempty"` // when confidence < threshold
}

type StudentCandidate struct {
    Name  string `json:"name"`
    Class string `json:"class"`
}
```

**Implementation (`gptExtractor`):**
- Calls OpenAI GPT-5.4 mini (via existing `go-openai` SDK + `OPENAI_API_KEY`) with a structured system prompt:
  - Input: transcript text + full student roster (class + name pairs)
  - Instructions: extract mentioned student name(s), topic of observation, brief summary, date if mentioned
  - Output format: JSON matching `ExtractResponse`
- Fuzzy matching is delegated to OpenAI (it sees the full roster and can match phonetic/partial names). OpenAI returns `confidence` per student.
- If the model can't confidently match a name (confidence < 0.7), it includes `candidates` — the top 3 closest roster matches.
- Prompt should instruct the model to handle multi-student transcripts by returning a **separate summary per student**.

**Prompt design notes:**
- System prompt includes the full roster as a reference table
- Tells the model to match mentioned names against the roster even if pronunciation differs
- For multi-student transcripts, produces an individual 1-3 sentence summary per student (suitable for that student's report card)
- Uses OpenAI **structured outputs** with a JSON schema defining the `ExtractResponse` shape — guarantees valid, parseable responses

---

### 2. Backend: Note creation service (`backend/notes.go`)

New file. Creates Google Docs and manages the metadata index.

```go
// NoteCreator creates note documents and updates the metadata index.
type NoteCreator interface {
    CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error)
}

type CreateNoteRequest struct {
    StudentName string
    ClassName   string
    Topic       string
    Summary     string
    Transcript  string
    Date        string // YYYY-MM-DD
}

type CreateNoteResponse struct {
    DocID  string `json:"docId"`
    DocURL string `json:"docUrl"`
}
```

**Implementation (`driveNoteCreator`):**
- Uses Google Docs API to create a Google Doc (not Drive raw file) in `GradeBee/notes/{class}/{student}/` subfolder
  - Two-level hierarchy: class folder, then student folder within it
  - Subfolder created on first note for that student (via Drive API `Files.List` filtered by name + parent)
  - Look up subfolder by name each time (no cached IDs — simpler, one fewer file to manage)
  - Folder names use unsanitized display names (e.g. `5A/Emma Johnson/`)
- Doc structure (using Docs API `batchUpdate` with insert requests):
  - **Title**: `{Student Name} — {Topic} ({Date})`
  - **Heading 1**: Topic
  - **Body**: Summary paragraph
  - **Heading 2**: "Transcript"
  - **Body**: Full transcript text
  - **Heading 2**: "Teacher Feedback"
  - **Body**: _(empty paragraph — teacher fills this in)_

No per-student metadata index. Phase 5 will list docs in the subfolder and read them directly. Doc titles encode date + topic for quick scanning. Optimize later if needed.

**Google Docs API**: Need to add `docs/v1` to google API services. Update `google.go` to create a `*docs.Service` alongside Drive and Sheets.

---

### 3. Backend: Wire into `deps` interface

**File:** `backend/deps.go`

Add to `deps` interface:
```go
GetExtractor() (Extractor, error)
GetNoteCreator(svc *googleServices) NoteCreator
```

Add to `prodDeps`:
- `GetExtractor()` — creates `gptExtractor` using `OPENAI_API_KEY` env var
- `GetNoteCreator(svc)` — creates `driveNoteCreator` using svc.Drive + svc.Docs

Update `mockDepsAll` in `testutil_test.go` with stub implementations.

---

### 4. Backend: `POST /extract` endpoint (`backend/extract_handler.go`)

New file. Handles the extraction step (returns matches for frontend confirmation).

**Request:**
```json
{
  "transcript": "...",
  "fileId": "..." 
}
```

**Response:**
```json
{
  "students": [
    { "name": "Emma Johnson", "class": "5A", "summary": "Emma demonstrated strong analytical skills during the reading exercise...", "confidence": 0.95, "candidates": [] }
  ],
  "topic": "Reading comprehension",
  "date": "2026-03-19"
}
```

**Flow:**
1. Authenticate user, get Google services
2. Load roster via `serviceDeps.GetRoster()`
3. Call `serviceDeps.GetExtractor().Extract()` with transcript + roster
4. Return structured response for frontend confirmation UI

---

### 5. Backend: `POST /notes` endpoint (`backend/notes_handler.go`)

New file. Creates the confirmed note(s).

**Request** (after user confirms/corrects extraction):
```json
{
  "fileId": "...",
  "students": [
    { "name": "Emma Johnson", "class": "5A", "summary": "Emma demonstrated strong analytical skills..." }
  ],
  "topic": "Reading comprehension",
  "transcript": "...",
  "date": "2026-03-19"
}
```

**Response:**
```json
{
  "notes": [
    { "student": "Emma Johnson", "class": "5A", "docId": "abc123", "docUrl": "https://docs.google.com/document/d/abc123/edit" }
  ]
}
```

**Flow:**
1. Authenticate user, get Google services
2. Get metadata (need `NotesID` folder)
3. For each student in the request:
   - Call `NoteCreator.CreateNote()` — creates subfolder if needed, creates doc
4. Return created note references

---

### 6. Backend: Register new routes in `handler.go`

**File:** `backend/handler.go`

- Add `extractHandler` and `notesHandler` vars (with auth middleware)
- Add switch cases: `path == "extract" && POST` → `extractHandler`, `path == "notes" && POST` → `notesHandler`

---

### 7. Backend: Add Google Docs service to `google.go`

**File:** `backend/google.go`

- Add `Docs *docs.Service` field to `googleServices` struct
- Create it in `newGoogleServices()` alongside Drive and Sheets
- Import `google.golang.org/api/docs/v1`

---

### 8. Frontend: API functions

**File:** `frontend/src/api.ts`

Add:
```typescript
export interface MatchedStudent {
  name: string
  class: string
  summary: string
  confidence: number
  candidates?: { name: string; class: string }[]
}

export interface ExtractResult {
  students: MatchedStudent[]
  topic: string
  date: string
}

export async function extractFromTranscript(
  transcript: string,
  fileId: string,
  getToken: () => Promise<string | null>
): Promise<ExtractResult> { ... }

export interface CreateNotesRequest {
  fileId: string
  students: { name: string; class: string; summary: string }[]
  topic: string
  transcript: string
  date: string
}

export interface NoteResult {
  student: string
  class: string
  docId: string
  docUrl: string
}

export async function createNotes(
  req: CreateNotesRequest,
  getToken: () => Promise<string | null>
): Promise<{ notes: NoteResult[] }> { ... }
```

---

### 9. Frontend: NoteConfirmation component

**File:** `frontend/src/components/NoteConfirmation.tsx`

Replaces the current "done" state in `AudioUpload`. After transcription completes, the flow continues:

1. **Extracting state**: Show honeycomb spinner + "Analyzing transcript..."
2. **Confirmation UI**: Card showing:
   - **Matched student(s)**: Each with a confidence badge and editable summary
     - High confidence (≥0.7): shown as confirmed, with "Change" link
     - Low confidence (<0.7): dropdown to pick from candidates or full roster
     - Each student has its own editable summary textarea (pre-filled from extraction)
   - **Topic**: Editable text input (pre-filled from extraction)
   - **Date**: Date input (pre-filled)
   - **Transcript**: Collapsible section showing full transcript (read-only)
   - **"Save Note" button**: Creates the note
   - **"Cancel" link**: Goes back to upload idle state
3. **Saving state**: Spinner + "Creating note..."
4. **Success state**: "Note created!" with link to open the Google Doc + "Upload another" button

Design follows `DESIGN.md`: cards with warm shadows, honey accents, Fraunces headings, motion transitions between states.

---

### 10. Frontend: Refactor AudioUpload flow

**File:** `frontend/src/components/AudioUpload.tsx`

The component's state machine expands:

`idle → uploading → transcribing → extracting → confirming → saving → saved → idle`

- After `transcribing` completes, auto-trigger extraction API call (→ `extracting`)
- On extraction success → `confirming` (show NoteConfirmation)
- On "Save Note" → `saving` → `saved`
- On "Upload another" → `idle`

The `NoteConfirmation` component can be rendered inline within `AudioUpload` or extracted as a child. I recommend keeping it as a separate component that receives props (`extractResult`, `transcript`, `fileId`, `onSave`, `onCancel`).

---

### 11. Tests

#### Backend unit tests

**`backend/extract_test.go`** — new file:
- Test `handleExtract` with mock extractor returning various results
- Test missing transcript / missing fileId → 400
- Test extractor error → 500
- Test roster unavailable → still attempts extraction with empty roster (graceful degradation)

**`backend/notes_test.go`** — new file:
- Test `handleCreateNotes` happy path — mock note creator returns doc ID
- Test missing required fields → 400
- Test note creator error → 500
- Test multi-student note creation

**`backend/extract_handler_test.go`** — or inline in extract_test.go:
- Stub Extractor for handler tests

Update **`backend/testutil_test.go`**:
- Add `stubExtractor` and `stubNoteCreator`
- Update `mockDepsAll` with new methods

#### Frontend (future — no e2e test changes required for plan)
- The existing e2e tests don't cover post-transcription flow yet. Can be added after implementation.

---

## File Summary

| File | Action |
|---|---|
| `backend/extract.go` | **New** — Extractor interface + GPT-5.4 mini impl |
| `backend/extract_handler.go` | **New** — POST /extract handler |
| `backend/extract_test.go` | **New** — handler + extraction tests |
| `backend/notes.go` | **New** — NoteCreator interface + Drive impl |
| `backend/notes_handler.go` | **New** — POST /notes handler |
| `backend/notes_test.go` | **New** — handler + note creation tests |
| `backend/deps.go` | Edit — add GetExtractor, GetNoteCreator |
| `backend/handler.go` | Edit — register /extract, /notes routes |
| `backend/google.go` | Edit — add Docs service to googleServices |
| `backend/testutil_test.go` | Edit — add stubs for new interfaces |
| `frontend/src/api.ts` | Edit — add extract + createNotes functions |
| `frontend/src/components/NoteConfirmation.tsx` | **New** — confirmation/editing UI |
| `frontend/src/components/AudioUpload.tsx` | Edit — extend state machine, integrate NoteConfirmation |

## Decisions

1. **One note per student**: A transcript mentioning 3 students creates 3 separate Google Docs with shared topic/date but individual summaries. The OpenAI extraction prompt returns per-student summaries. Uses structured outputs (JSON schema) for reliable parsing.

2. **Subfolder naming**: `notes/{class}/{student}/` using unsanitized display names (e.g. `notes/5A/Emma Johnson/`). Drive allows any characters in folder names. Subfolder found via `Files.List` by name each time.

## Open Questions

1. **Google Docs API scope**: The Docs API uses `https://www.googleapis.com/auth/documents` scope. However, since we create the doc via Drive API first (under `drive.file` scope), we should be able to edit it with the Docs API using the same `drive.file` token. **Need to verify this works** — if not, we may need to create docs by writing HTML content via Drive API instead.
