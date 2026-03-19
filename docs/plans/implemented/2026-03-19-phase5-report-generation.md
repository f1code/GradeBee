# Phase 5: Report Card Generation — Implementation Plan

## Goal

Let teachers generate report cards by aggregating student notes over a date range. Reports match teacher-provided example report cards in style/layout. Teachers can supply additional per-generation instructions. Reports are created as Google Docs with a feedback section for iterative regeneration.

---

## 1. Metadata Index (prerequisite — backfill from Phase 4)

Phase 4's note creation already stores Google Docs but no metadata index exists yet. Reports depend on it for efficient note lookup.

### 1a. Index format

Each student gets `GradeBee/notes/{class}_{student}/index.json` stored as a plain file in Drive (not a Google Doc). Structure:

```json
{
  "entries": [
    {
      "docId": "1abc...",
      "date": "2026-02-15",
      "summary": "Showed strong improvement in reading fluency..."
    }
  ]
}
```

### 1b. Backend: `MetadataIndex` interface

**File:** `backend/metadata_index.go`

```go
type IndexEntry struct {
    DocID   string `json:"docId"`
    Date    string `json:"date"`
    Summary string `json:"summary"`
}

type StudentIndex struct {
    Entries []IndexEntry `json:"entries"`
}

type MetadataIndex interface {
    ReadIndex(ctx context.Context, notesRootID, class, student string) (*StudentIndex, error)
    AppendEntry(ctx context.Context, notesRootID, class, student string, entry IndexEntry) error
}
```

Production impl `driveMetadataIndex` — reads/writes `index.json` via Drive API. Uses `findOrCreateFolder` (already in `notes.go`, extract to shared util).

### 1c. Update `NoteCreator.CreateNote` to write index

After creating the Google Doc, call `MetadataIndex.AppendEntry`. Add `MetadataIndex` as a field on `driveNoteCreator`.

### 1d. DI wiring

Add `GetMetadataIndex(svc) MetadataIndex` to `deps` interface. Wire in `prodDeps`.

---

## 2. Example Report Cards — Storage & Management

### 2a. Drive folder

`POST /setup` creates `GradeBee/report-examples/` subfolder. Add `ReportExamplesID` to `gradeBeeMetadata`.

**File:** `backend/setup.go` — add folder creation.
**File:** `backend/clerk_metadata.go` — add `metaKeyReportExamplesID` + field.

### 2b. Backend: Upload example

**Endpoint:** `POST /report-examples`
**File:** `backend/report_examples_handler.go`

- Accepts multipart upload (text file) or JSON body with pasted text.
- Text-only for now (no images/PDFs).
- Stores as plain text file in `GradeBee/report-examples/` on Drive.
- Returns `{ id, name }`.

### 2c. Backend: List & delete examples

**Endpoint:** `GET /report-examples` — list files in the examples folder.
**Endpoint:** `DELETE /report-examples/{id}` — trash a file.
**File:** `backend/report_examples_handler.go`

### 2d. Backend: Read examples for prompt construction

**File:** `backend/report_examples.go`

```go
type ReportExample struct {
    ID      string
    Name    string
    Content string // plain text content
}

type ExampleStore interface {
    ListExamples(ctx context.Context, examplesFolderID string) ([]ReportExample, error)
}
```

Reads each file's content as plain text via Drive export/download.

### 2e. Frontend: Example management UI

**File:** `frontend/src/components/ReportExamples.tsx`

- Card-based UI inside report settings area.
- Drag-drop or file picker to upload example report cards (text files).
- Shows names of uploaded examples with delete button.
- Persisted on Drive — loads on mount via `GET /report-examples`.

### 2f. Frontend: API functions

**File:** `frontend/src/api.ts` — add `uploadReportExample`, `listReportExamples`, `deleteReportExample`.

---

## 3. Report Generation

### 3a. Backend: `ReportGenerator` interface

**File:** `backend/report_generator.go`

```go
type GenerateReportRequest struct {
    Student        string
    Class          string
    StartDate      string // YYYY-MM-DD
    EndDate        string // YYYY-MM-DD
    NotesRootID    string
    ReportsID      string
    ExamplesFolderID string
    Instructions   string // free-text teacher instructions
}

type GenerateReportResponse struct {
    DocID    string `json:"docId"`
    DocURL   string `json:"docUrl"`
    Skipped  bool   `json:"skipped"`  // true if report already existed
}

type ReportGenerator interface {
    Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error)
}
```

Production impl `gptReportGenerator` holds: `*openai.Client`, `MetadataIndex`, `ExampleStore`, Drive/Docs services.

**Flow:**
1. Resolve or create `GradeBee/reports/{YYYY-MM}/` subfolder (YYYY-MM from endDate).
2. Check if a Google Doc named `"{StudentName} — {Class}"` already exists in that folder. If so, return early with `Skipped: true` and the existing doc ID/URL.
3. `MetadataIndex.ReadIndex` → filter entries where `startDate <= entry.Date <= endDate`.
4. Collect summaries from filtered entries (no full doc fetch).
5. `ExampleStore.ListExamples` → load example report cards.
6. Build GPT prompt (see §3b).
7. Call GPT → get report narrative.
8. Create Google Doc in the reports subfolder.
9. Populate doc: report narrative + "Teacher Feedback" heading + empty paragraph.
10. Return doc ID/URL with `Skipped: false`.

### 3b. GPT prompt design

**File:** `backend/report_prompt.go`

System prompt structure:
```
You are a report card writer for a school teacher.

## Style & Layout Guide
[If examples provided:]
The following are example report cards. Match their tone, voice, vocabulary,
section structure, and approximate length.

[For each example: inline the text]

[If no examples:]
Write a professional, warm report card narrative.

## Additional Instructions
{teacher's free-text instructions, if any}

## Student Notes
Student: {name}, Class: {class}
Period: {startDate} to {endDate}

{For each note entry:}
- {date}: {summary}

## Task
Write a report card narrative for this student based on the notes above.
Follow the style and layout of the examples provided.
```

### 3c. Backend: `POST /reports` endpoint

**File:** `backend/reports_handler.go`

Request body:
```json
{
  "students": [
    { "name": "Alice", "class": "3A" }
  ],
  "startDate": "2026-01-01",
  "endDate": "2026-03-15",
  "instructions": "Focus on social development"
}
```

- Iterates over students **sequentially**.
- If a report already exists for a student (same name + class in the `YYYY-MM` folder), marks it as `skipped` and includes the existing doc link.
- **Stops on first failure** — returns the error plus all reports generated so far (both new and skipped).
- Response:

```json
{
  "reports": [
    { "student": "Alice", "class": "3A", "docId": "...", "docUrl": "...", "skipped": false },
    { "student": "Bob", "class": "3A", "docId": "...", "docUrl": "...", "skipped": true }
  ],
  "error": null
}
```

If a failure occurs mid-batch:
```json
{
  "reports": [ /* successfully completed so far */ ],
  "error": "failed to generate report for Charlie: ..."
}
```

HTTP status is **200** even on partial failure (so the client can read the partial results). Status **4xx/5xx** only for request validation or auth errors.

### 3d. DI wiring

Add to `deps` interface:
- `GetReportGenerator(svc) ReportGenerator`
- `GetExampleStore(svc) ExampleStore`

Add handler var + route in `handler.go`.

---

## 4. Report Regeneration

### 4a. Backend: Read feedback from Google Doc

**File:** `backend/report_generator.go` (method on `gptReportGenerator`)

`readFeedback(ctx, docID)` — fetches Google Doc, finds "Teacher Feedback" heading, extracts text below it.

### 4b. Backend: `POST /reports/regenerate`

**File:** `backend/reports_handler.go`

Request body:
```json
{
  "docId": "1abc...",
  "student": "Alice",
  "class": "3A",
  "startDate": "2026-01-01",
  "endDate": "2026-03-15",
  "instructions": "Focus on social development"
}
```

**Flow:**
1. Read feedback from existing report doc.
2. Read notes index (same as generate).
3. Load examples (same as generate).
4. Build prompt with feedback appended: `"## Teacher Feedback on Previous Draft\n{feedback}"`.
5. Call GPT → new narrative.
6. Replace report doc content (clear body, repopulate). Preserves same doc ID/URL.

---

## 5. Frontend: Report Generation UI

### 5a. Report page component

**File:** `frontend/src/components/ReportGeneration.tsx`

**Sections:**
1. **Period picker** — start date / end date inputs (styled per DESIGN.md).
2. **Student/class selector** — checkboxes grouped by class. "Select all" per class.
3. **Example report cards** — inline `<ReportExamples />` component (from §2e). Collapsible, shows count badge.
4. **Additional instructions** — `<textarea>` with placeholder "e.g. Focus on social skills, keep paragraphs short...". Not persisted.
5. **Generate button** — triggers `POST /reports`. Shows honeycomb spinner during generation.
6. **Results** — list of generated reports with links to Google Docs.

### 5b. Integration in App.tsx

Add a tab/section toggle or navigation for "Reports" alongside existing student list + upload flow. Could be a simple toolbar link (`.toolbar-link` per DESIGN.md).

### 5c. API functions

**File:** `frontend/src/api.ts`

```ts
export async function generateReports(req: {
  students: { name: string; class: string }[]
  startDate: string
  endDate: string
  instructions?: string
}, getToken: () => Promise<string | null>): Promise<{ reports: ReportResult[] }>

export async function regenerateReport(req: {
  docId: string
  student: string
  class: string
  startDate: string
  endDate: string
  instructions?: string
}, getToken: () => Promise<string | null>): Promise<ReportResult>
```

---

## 6. Route & Handler Registration

**File:** `backend/handler.go`

New routes:
| Method | Path | Handler |
|--------|------|---------|
| GET | `/report-examples` | `handleListReportExamples` |
| POST | `/report-examples` | `handleUploadReportExample` |
| DELETE | `/report-examples` | `handleDeleteReportExample` |
| POST | `/reports` | `handleGenerateReports` |
| POST | `/reports/regenerate` | `handleRegenerateReport` |

---

## File Summary

### New backend files
| File | Purpose |
|------|---------|
| `metadata_index.go` | `MetadataIndex` interface + Drive impl |
| `report_examples.go` | `ExampleStore` interface + Drive impl |
| `report_examples_handler.go` | `GET/POST/DELETE /report-examples` handlers |
| `report_generator.go` | `ReportGenerator` interface + GPT impl, feedback reader |
| `report_prompt.go` | Prompt construction for report generation |
| `reports_handler.go` | `POST /reports` + `POST /reports/regenerate` handlers |

### Modified backend files
| File | Change |
|------|--------|
| `handler.go` | Add 5 new routes |
| `deps.go` | Add `GetMetadataIndex`, `GetReportGenerator`, `GetExampleStore` |
| `clerk_metadata.go` | Add `ReportExamplesID` field + key |
| `setup.go` | Create `report-examples/` folder |
| `notes.go` | Inject `MetadataIndex`, call `AppendEntry` after doc creation; extract `findOrCreateFolder` to shared util |

### New frontend files
| File | Purpose |
|------|---------|
| `components/ReportExamples.tsx` | Example report card management UI |
| `components/ReportGeneration.tsx` | Main report generation page |

### Modified frontend files
| File | Change |
|------|--------|
| `api.ts` | Add report + example API functions |
| `App.tsx` | Add Reports navigation/section |

### Test files
| File | Covers |
|------|--------|
| `metadata_index_test.go` | Index read/write/append |
| `report_examples_test.go` | Example CRUD handlers |
| `reports_test.go` | Generate + regenerate handlers |

---

## Implementation Order

1. **Metadata index** (§1) — foundation, also improves Phase 4
2. **Setup update** (§2a) — `report-examples/` folder + metadata field
3. **Example store + handlers** (§2b–2d) — backend CRUD for examples
4. **Report generator + prompt** (§3a–3b) — core generation logic
5. **Report handlers** (§3c–3d, §4) — generate + regenerate endpoints
6. **Frontend: API layer** (§2f, §5c) — all new API functions
7. **Frontend: ReportExamples component** (§2e)
8. **Frontend: ReportGeneration component** (§5a–5b)

---

## Open Questions

None — all resolved.
