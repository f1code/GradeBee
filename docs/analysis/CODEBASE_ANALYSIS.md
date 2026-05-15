# GradeBee Codebase Analysis & Bug Report Feature Implementation Guide

## Executive Summary

GradeBee is a full-stack teacher tool that enables voice note recording and automatic report generation. It uses:
- **Backend**: Go 1.24 with plain `net/http` (no framework)
- **Frontend**: React 19 + TypeScript with Vite
- **Database**: SQLite with WAL mode
- **Authentication**: Clerk JWT
- **AI**: OpenAI Whisper (transcription) + Claude (report generation)

The codebase is well-structured with clear separation of concerns, making it straightforward to add new features like a bug report system.

---

## 1. PROJECT STRUCTURE

```
GradeBee/
├── backend/                        # Go HTTP backend
│   ├── cmd/server/main.go          # Server entrypoint
│   ├── handler.go                  # Main HTTP router
│   ├── deps.go                     # Dependency injection
│   ├── db.go                       # SQLite setup (WAL mode)
│   ├── migrate.go                  # Migration runner
│   ├── sql/                        # SQL migration files
│   ├── repo_*.go                   # Repository layer (CRUD)
│   ├── students.go                 # Class/student handlers
│   ├── notes.go                    # Note handlers & NoteCreator interface
│   ├── transcriber.go              # Whisper integration
│   ├── extract.go                  # GPT extraction interface
│   ├── report_*.go                 # Report generation handlers
│   ├── job_queue.go                # Generic async job queue
│   ├── voice_note_*.go             # Voice note pipeline
│   ├── Makefile                    # Build targets (lint, test, generate)
│   └── tygo.yaml                   # Go→TypeScript type generation
│
├── frontend/                       # React SPA
│   ├── src/
│   │   ├── api.ts                  # API client (calls backend handlers)
│   │   ├── api-types.gen.ts        # Generated types from Go structs
│   │   ├── App.tsx                 # Root component (routing)
│   │   ├── components/             # Feature components
│   │   │   ├── StudentList.tsx
│   │   │   ├── AudioUpload.tsx
│   │   │   ├── NoteEditor.tsx
│   │   │   ├── ReportGeneration.tsx
│   │   │   └── ...
│   │   ├── hooks/                  # Custom hooks (useDrivePicker, useMediaQuery)
│   │   └── test/                   # Test setup
│   ├── DESIGN.md                   # Design system (colors, typography, components)
│   ├── vite.config.ts
│   └── vitest.config.ts
│
├── sql/                            # Not in backend/sql; migrations live there
├── e2e/                            # Playwright tests
├── docs/                           # Implementation plans
├── Makefile                        # Top-level build, deploy, test
└── .env.example                    # Environment template
```

---

## 2. BACKEND ARCHITECTURE

### 2.1 HTTP Routing Pattern

**File**: `backend/handler.go`

The backend uses a single `Handle(w, r)` function that:
1. Logs request metadata in a request-scoped context
2. Extracts path and checks against patterns with `strings.HasPrefix` + `pathParam()`
3. Routes to appropriate handler with auth middleware wrapping
4. Returns JSON responses via `writeJSON()` helper

**Example routing**:
```go
case path == "classes" && r.Method == http.MethodGet:
    authHandler(handleListClasses).ServeHTTP(rec, r)

case strings.HasPrefix(path, "classes/") && strings.HasSuffix(path, "/students") && r.Method == http.MethodPost:
    authHandler(handleCreateStudent).ServeHTTP(rec, r)
```

Auth is Clerk JWT via `clerkhttp.RequireHeaderAuthorization()` middleware.

### 2.2 API Endpoints (38 total)

Key patterns:
- **Classes**: GET `/classes`, POST `/classes`, PUT `/classes/{id}`, DELETE `/classes/{id}`
- **Students**: GET `/students`, POST `/classes/{id}/students`, PUT `/students/{id}`, DELETE `/students/{id}`
- **Notes**: GET `/students/{id}/notes`, POST `/students/{id}/notes`, GET `/notes/{id}`, PUT `/notes/{id}`, DELETE `/notes/{id}`
- **Reports**: POST `/reports`, POST `/reports/{id}/regenerate`, GET `/students/{id}/reports`, GET `/reports/{id}`, DELETE `/reports/{id}`
- **Voice Notes**: POST `/voice-notes/upload`, GET `/voice-notes/jobs`, POST `/voice-notes/jobs/retry`, POST `/voice-notes/jobs/dismiss`

### 2.3 Dependency Injection

**File**: `backend/deps.go`

All services are injected through a `deps` interface (26 methods):
```go
type deps interface {
    GetTranscriber() Transcriber
    GetRoster(ctx, userID) Roster
    GetExtractor() Extractor
    GetNoteCreator() NoteCreator
    GetExampleStore() ExampleStore
    GetReportGenerator() ReportGenerator
    GetVoiceNoteQueue() JobQueue[VoiceNoteJob]
    GetDB() *sql.DB
    GetClassRepo() *ClassRepo
    GetStudentRepo() *StudentRepo
    GetNoteRepo() *NoteRepo
    GetReportRepo() *ReportRepo
    // ... 13 more
}
```

Production implementation: `prodDeps` (wires real OpenAI/Claude clients).
Tests override with stubs via `serviceDeps` package variable.

### 2.4 Database Schema

**File**: `backend/sql/001_init.sql` (+ 3 migrations)

Core tables:
- **classes**: id, user_id (FK), name, class_name, group_name, position, created_at
- **students**: id, class_id (FK), name, created_at
- **notes**: id, student_id (FK), date, summary (extracted passages), transcript, source, created_at, updated_at
- **reports**: id, student_id (FK), start_date, end_date, html, instructions, created_at
- **report_examples**: id, user_id, name, content, status, file_path, created_at
- **voice_notes**: id, user_id, file_name, file_path, processed_at, created_at
- **report_example_classes**: example_id (FK), class_name (M-M link)

Each table has user isolation (class ownership → student ownership → note ownership). Foreign keys with CASCADE delete enabled.

### 2.5 Repository Layer

**Pattern**: Each table has a `Repo*` type (e.g., `ClassRepo`, `StudentRepo`, `NoteRepo`)

Example: `ClassRepo` (repo_class.go)
```go
type ClassRepo struct{ db *sql.DB }

func (r *ClassRepo) List(ctx context.Context, userID string) ([]ClassWithCount, error) { ... }
func (r *ClassRepo) Create(ctx context.Context, userID, className, groupName string) (Class, error) { ... }
func (r *ClassRepo) Update(ctx context.Context, userID string, id int64, className, groupName string) error { ... }
func (r *ClassRepo) Delete(ctx context.Context, userID string, id int64) error { ... }
```

Error handling:
- `ErrNotFound` for missing entities
- `ErrDuplicate` for unique constraint violations
- Standard `fmt.Errorf` wrapping for context

### 2.6 Async Job Queue

**Files**: `backend/job_queue.go`, `backend/job_queue_mem.go`

Generic in-memory queue with worker pool:
```go
type Keyed interface {
    JobKey() string
    OwnerID() string
}

type JobQueue[T Keyed] interface {
    Publish(job T) error
    GetJob(ctx context.Context, key string) (T, error)
    UpdateJob(ctx context.Context, key string, status string, errorMsg string) error
    ListJobs(ctx context.Context, userID string) ([]T, error)
    DeleteJob(ctx context.Context, key string) error
    Close() error
}

type MemQueue[T Keyed] struct { ... }
```

Usage: Transcription, extraction, and report example processing all use this pattern.

### 2.7 Handler Pattern

**File**: `backend/students.go` (example)

```go
func handleCreateClass(w http.ResponseWriter, r *http.Request) {
    // 1. Extract user ID from Clerk JWT
    userID, err := userIDFromRequest(r)
    if err != nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
        return
    }
    
    // 2. Decode JSON request body
    var req struct {
        ClassName string `json:"className"`
        Group     string `json:"group"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClassName == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "className is required"})
        return
    }
    
    // 3. Call repository through DI
    c, err := serviceDeps.GetClassRepo().Create(r.Context(), userID, req.ClassName, req.Group)
    if err != nil {
        if errors.Is(err, ErrDuplicate) {
            writeJSON(w, http.StatusConflict, map[string]string{"error": "class already exists"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    // 4. Return JSON response
    writeJSON(w, http.StatusCreated, ClassWithCount{Class: c, StudentCount: 0})
}
```

**Common patterns**:
- Error checks with user context validation
- Repository CRUD through `serviceDeps`
- `writeJSON(w, status, response)` for all responses
- Status codes: 200 OK, 201 Created, 400 Bad Request, 404 Not Found, 409 Conflict, 500 Internal Server Error

### 2.8 Authorization

All CRUD endpoints verify resource ownership:
1. Extract `userID` from Clerk JWT claims
2. For class operations: query class, verify `class.UserID == userID`
3. For student/note/report operations: join through ownership chain

Example (notes.go):
```go
owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), studentID, userID)
if err != nil || !owns {
    writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
    return
}
```

### 2.9 Error Handling

**File**: `backend/google.go`

`apiError` struct (rarely used, mostly for Drive integration):
```go
type apiError struct {
    Status  int
    Err     error
    Code    string // machine-readable
    Message string // human-readable
}

func writeAPIError(w http.ResponseWriter, r *http.Request, err *apiError) {
    // Logs and writes JSON with error code + message
}
```

Standard errors via `errors.Is(err, ErrNotFound)` or `errors.Is(err, ErrDuplicate)`.

---

## 3. FRONTEND ARCHITECTURE

### 3.1 Tech Stack

- **React 19** with hooks (useState, useEffect, useCallback)
- **TypeScript** for type safety
- **Vite** for build/dev
- **react-router-dom v7** for routing (not visible in current setup, but in package.json)
- **Clerk React SDK** for authentication
- **motion (Framer Motion)** for animations
- **fetch API** for HTTP calls (no Axios/fetch wrapper)

### 3.2 Project Structure

```
frontend/src/
├── api.ts                     # All API call functions (exported)
├── api-types.gen.ts          # Generated Go→TypeScript types
├── App.tsx                   # Root component (auth gate, tab routing)
├── main.tsx                  # Entry point
├── index.css                 # Global styles (design system tokens as CSS vars)
├── components/
│   ├── StudentList.tsx       # Main class/student UI
│   ├── AudioUpload.tsx       # Upload + drag-drop
│   ├── NoteEditor.tsx        # Note creation/editing modal
│   ├── NotesList.tsx         # Notes display for a student
│   ├── ReportGeneration.tsx  # Report generation UI
│   ├── ReportViewer.tsx      # HTML report display
│   ├── ReportExamples.tsx    # Example report management
│   ├── JobStatus.tsx         # Upload job status indicator
│   ├── AddClassForm.tsx      # Class creation modal
│   ├── AddStudentForm.tsx    # Student creation modal
│   ├── StudentDetail.tsx     # Student detail view (notes + reports tabs)
│   ├── Icons.tsx             # Reusable SVG icons
│   ├── HintBanner.tsx        # Informational banner
│   └── __tests__/            # Component tests
├── hooks/
│   ├── useDrivePicker.ts     # Google Drive Picker integration
│   └── useMediaQuery.ts      # Responsive breakpoint detection
└── test/
    ├── setup.ts              # Vitest setup
    └── mocks.ts              # API mocks
```

### 3.3 API Client Pattern

**File**: `frontend/src/api.ts`

All API calls are pure async functions. Pattern:
```typescript
export async function listClasses(
    getToken: () => Promise<string | null>
): Promise<{ classes: ClassItem[] }> {
    const token = await getToken()
    const resp = await fetch(`${apiUrl}/classes`, {
        headers: { Authorization: `Bearer ${token}` },
    })
    const body = await resp.json()
    if (!resp.ok) throw new Error(body.error || 'Failed to list classes')
    return body
}
```

**Pattern breakdown**:
- Each function takes `getToken` as parameter (Clerk hook from component)
- Constructs `Authorization: Bearer <token>` header
- Throws error if `!resp.ok`
- Returns parsed JSON response
- Error messages from backend's `error` field

### 3.4 Component Pattern

**File**: `frontend/src/components/AddClassForm.tsx` (example)

```typescript
interface AddClassFormProps {
    onCreated: (cls: ClassItem) => void
    onCancel?: () => void
}

export default function AddClassForm({ onCreated, onCancel }: AddClassFormProps) {
    const { getToken } = useAuth()
    const [className, setClassName] = useState('')
    const [error, setError] = useState<string | null>(null)
    const [submitting, setSubmitting] = useState(false)

    async function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setSubmitting(true)
        setError(null)
        try {
            const cls = await createClass(className, group, getToken)
            onCreated(cls)
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to create class')
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <motion.div className="add-class-form" {...animations}>
            <form onSubmit={handleSubmit}>
                {/* Form fields */}
            </form>
            {error && <p className="add-form-error">{error}</p>}
        </motion.div>
    )
}
```

**Pattern breakdown**:
- Props are typed interfaces
- Clerk's `useAuth()` provides `getToken`
- State: form values, `error` (string | null), `submitting` (boolean)
- `try/catch` in event handlers; errors set to state for display
- Motion animations for entrance/exit
- Conditional rendering of error message

### 3.5 Design System

**File**: `frontend/DESIGN.md` + `frontend/src/index.css`

CSS custom properties:
```css
--honey: #E8A317 (primary accent, buttons)
--honey-dark: #C4880F (hover/pressed)
--honey-light: #FFF3D4 (backgrounds)
--comb: #F5E6C8 (card backgrounds)
--ink: #2C1810 (primary text)
--ink-muted: #7A6B5D (secondary text)
--parchment: #FBF7F0 (page background)
--chalk: #FFFFFF (card surfaces)
--error-red: #C53030 (errors)
--success-green: #38A169 (success)
```

Fonts:
- **Display**: Fraunces (serif, warm)
- **Body**: Source Sans 3 (sans-serif, readable)

Component patterns:
- Buttons: Default is primary (`background: var(--honey)`), `.btn-secondary`, `.btn-danger`
- Cards: `background: var(--chalk)`, `border-radius: 12px`, warm shadow
- Drop zone: Dashed `--honey` border, `--comb` background
- Animations: `motion` library for stagger/transitions

### 3.6 Type Generation

**Files**: `backend/tygo.yaml` → generates → `frontend/src/api-types.gen.ts`

Go structs with `json` tags are converted to TypeScript interfaces:
```go
// backend/repo_class.go
type ClassWithCount struct {
    Class `tstype:",extends"` // Flattens to inline props
    StudentCount int `json:"studentCount"`
}
```

Generated:
```typescript
export interface ClassWithCount extends Class {
    studentCount: number
}
```

**Regenerate**: `cd backend && make generate` (then commit `.gen.ts`)

### 3.7 Responsive Design

Breakpoints:
- `480px` (sm): Mobile portrait, stack vertically
- `640px` (md): Mobile landscape/tablet, full-width nav
- `860px` (lg): Desktop, max content width

Touch targets: 44×44px minimum on mobile.

---

## 4. DATABASE SCHEMA & MODELS

### 4.1 Core Data Model

```
User (via Clerk, no DB row)
  └─ classes (user_id)
      └─ students (class_id)
          ├─ notes (student_id, date, source: 'auto' or 'manual')
          └─ reports (student_id, start_date, end_date, html)
      
report_examples (user_id, for style matching)
  └─ report_example_classes (M-M: example_id, class_name)

voice_notes (user_id, file tracking, processed_at)
```

### 4.2 Key Table Structures

**classes**:
```sql
id, user_id, name, class_name, group_name, position, created_at
UNIQUE(user_id, class_name, group_name)
```

**students**:
```sql
id, class_id (FK→classes), name, created_at
UNIQUE(class_id, name)
```

**notes**:
```sql
id, student_id (FK→students), date, summary, transcript, source, created_at, updated_at
Indexes: (student_id), (student_id, date)
```

**reports**:
```sql
id, student_id (FK→students), start_date, end_date, html, instructions, created_at
```

**voice_notes** (for audio file tracking):
```sql
id, user_id, file_name, file_path, processed_at, created_at
```

### 4.3 Authorization Checks

Every CRUD operation verifies:
1. User extracted from Clerk JWT
2. Class: `class.user_id == userID`
3. Student: `exists in user's classes`
4. Note/Report: `student belongs to user`

No row-level audit columns; deletions are cascade.

---

## 5. CURRENT FEATURE EXAMPLES

### 5.1 Class CRUD (Complete Feature)

**Backend** (`backend/students.go`):
1. `handleListClasses` → GET `/classes`
2. `handleCreateClass` → POST `/classes` (with duplicate check)
3. `handleUpdateClass` → PUT `/classes/{id}`
4. `handleDeleteClass` → DELETE `/classes/{id}`

All route through `ClassRepo` (dependency-injected).

**Frontend** (`frontend/src/components/AddClassForm.tsx`):
1. Form component with controlled inputs
2. Calls `createClass(className, group, getToken)` from `api.ts`
3. Handles error state, disables form while submitting
4. Callback to parent on success

**Pattern to replicate**: Request → Handler → Auth check → Repo CRUD → Response.

### 5.2 Voice Note Upload Pipeline (Async Job)

**Backend**:
1. POST `/voice-notes/upload` saves file to disk, creates `voice_notes` row
2. Dispatches `VoiceNoteJob` to `MemQueue` (job_queue_mem.go)
3. Worker goroutine processes:
   - Step 1: Transcribe via Whisper
   - Step 2: Extract student observations via GPT
   - Step 3: Create notes for each student (NoteCreator interface)
4. Frontend polls GET `/voice-notes/jobs` for status

**Files**:
- `backend/voice_note_upload.go` – handler
- `backend/voice_note_job.go` – job type with status
- `backend/voice_note_process.go` – processing pipeline
- `backend/voice_note_jobs.go` – job list/retry handlers
- `frontend/src/components/JobStatus.tsx` – status display + polling

### 5.3 Report Generation (Multi-step)

**Backend**:
1. POST `/reports` → `handleGenerateReports`
2. Collects notes for students (date range)
3. Calls `ReportGenerator.Generate()` (interface, GPT-backed)
4. Stores HTML in `reports` table
5. Returns `ReportResult[]` with HTML

**Frontend** (`frontend/src/components/ReportGeneration.tsx`):
1. Date range picker
2. Student selector
3. "Generate" button triggers API call
4. Poll for completion (no explicit job tracking here; returns immediately)
5. Display HTML in `ReportViewer` component

**Pattern**: Synchronous endpoint that calls external AI service.

### 5.4 Report Example Extraction (Async with Polling)

**Backend**:
1. POST `/report-examples` saves file to disk, creates `report_examples` row with `status='processing'`
2. Dispatches `ExtractionJob` to separate `MemQueue`
3. Worker:
   - Converts PDF → JPEGs via pdftoppm
   - Sends pages to GPT Vision (parallel)
   - Updates row: `status='ready'`, `content=extracted_text`
4. Frontend polls GET `/report-examples` every 3s while `status='processing'`

**Files**:
- `backend/report_examples_handler.go` – handler
- `backend/report_example_extractor.go` – GPT Vision integration
- `backend/report_example_job.go` – job type
- `backend/report_example_process.go` – pipeline
- `frontend/src/components/ReportExamples.tsx` – UI with polling

---

## 6. BUG REPORT FEATURE IMPLEMENTATION GUIDE

### 6.1 Where Bug Reports Fit

Bug reports are **user-generated feedback** about application issues, errors, or feature requests. Unlike notes (teacher→student observations), bug reports are user→app feedback.

Placement options:
1. **Separate collection** – Independent from class/student/note system
2. **Linked to context** – Optional student/class context (e.g., "Bug occurred while editing notes for Student X")

**Recommendation**: Start with separate collection (simpler), optional context later.

### 6.2 Database Schema

**Add migration** `backend/sql/005_bug_reports.sql`:
```sql
CREATE TABLE IF NOT EXISTS bug_reports (
    id          INTEGER PRIMARY KEY,
    user_id     TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL,
    stack_trace TEXT,                    -- Optional stack trace from frontend
    user_agent  TEXT,                    -- Browser info
    url         TEXT,                    -- Page where bug occurred
    status      TEXT NOT NULL DEFAULT 'open',  -- 'open', 'triaged', 'fixed', 'closed'
    severity    TEXT NOT NULL DEFAULT 'low',   -- 'low', 'medium', 'high', 'critical'
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_bug_reports_user ON bug_reports(user_id);
CREATE INDEX IF NOT EXISTS idx_bug_reports_status ON bug_reports(status);
CREATE INDEX IF NOT EXISTS idx_bug_reports_created ON bug_reports(created_at);
```

### 6.3 Backend Implementation

#### Step 1: Create Repository (`backend/repo_bug_report.go`)

```go
package handler

import (
    "context"
    "database/sql"
    "fmt"
)

type BugReportRepo struct{ db *sql.DB }

type BugReport struct {
    ID          int64  `json:"id"`
    UserID      string `json:"userId"`
    Title       string `json:"title"`
    Description string `json:"description"`
    StackTrace  *string `json:"stackTrace,omitempty"`
    UserAgent   *string `json:"userAgent,omitempty"`
    URL         *string `json:"url,omitempty"`
    Status      string `json:"status"`
    Severity    string `json:"severity"`
    CreatedAt   string `json:"createdAt"`
    UpdatedAt   string `json:"updatedAt"`
}

// List returns all bug reports for a user (most recent first)
func (r *BugReportRepo) List(ctx context.Context, userID string) ([]BugReport, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT id, user_id, title, description, stack_trace, user_agent, url, 
               status, severity, created_at, updated_at
        FROM bug_reports
        WHERE user_id = ?
        ORDER BY created_at DESC`, userID)
    if err != nil {
        return nil, fmt.Errorf("list bug reports: %w", err)
    }
    defer rows.Close()

    var result []BugReport
    for rows.Next() {
        var br BugReport
        if err := rows.Scan(&br.ID, &br.UserID, &br.Title, &br.Description, 
            &br.StackTrace, &br.UserAgent, &br.URL, &br.Status, &br.Severity, 
            &br.CreatedAt, &br.UpdatedAt); err != nil {
            return nil, fmt.Errorf("scan bug report: %w", err)
        }
        result = append(result, br)
    }
    return result, rows.Err()
}

// Create inserts a new bug report
func (r *BugReportRepo) Create(ctx context.Context, userID, title, description string,
    stackTrace, userAgent, url *string, severity string) (BugReport, error) {
    
    if severity == "" {
        severity = "low"
    }
    
    var br BugReport
    err := r.db.QueryRowContext(ctx, `
        INSERT INTO bug_reports (user_id, title, description, stack_trace, user_agent, url, severity)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        RETURNING id, user_id, title, description, stack_trace, user_agent, url,
                  status, severity, created_at, updated_at`,
        userID, title, description, stackTrace, userAgent, url, severity,
    ).Scan(&br.ID, &br.UserID, &br.Title, &br.Description, &br.StackTrace, 
        &br.UserAgent, &br.URL, &br.Status, &br.Severity, &br.CreatedAt, &br.UpdatedAt)
    
    if err != nil {
        return BugReport{}, fmt.Errorf("create bug report: %w", err)
    }
    return br, nil
}

// Get retrieves a single bug report (verify ownership)
func (r *BugReportRepo) Get(ctx context.Context, id int64, userID string) (BugReport, error) {
    var br BugReport
    err := r.db.QueryRowContext(ctx, `
        SELECT id, user_id, title, description, stack_trace, user_agent, url,
               status, severity, created_at, updated_at
        FROM bug_reports
        WHERE id = ? AND user_id = ?`, id, userID).Scan(
        &br.ID, &br.UserID, &br.Title, &br.Description, &br.StackTrace,
        &br.UserAgent, &br.URL, &br.Status, &br.Severity, &br.CreatedAt, &br.UpdatedAt)
    if err == sql.ErrNoRows {
        return BugReport{}, ErrNotFound
    }
    if err != nil {
        return BugReport{}, fmt.Errorf("get bug report: %w", err)
    }
    return br, nil
}

// UpdateStatus updates the status of a bug report
func (r *BugReportRepo) UpdateStatus(ctx context.Context, id int64, userID string, status string) error {
    res, err := r.db.ExecContext(ctx,
        "UPDATE bug_reports SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND user_id = ?",
        status, id, userID)
    if err != nil {
        return fmt.Errorf("update bug report status: %w", err)
    }
    return rowsAffectedOrNotFound(res)
}

// Delete removes a bug report
func (r *BugReportRepo) Delete(ctx context.Context, id int64, userID string) error {
    res, err := r.db.ExecContext(ctx,
        "DELETE FROM bug_reports WHERE id = ? AND user_id = ?", id, userID)
    if err != nil {
        return fmt.Errorf("delete bug report: %w", err)
    }
    return rowsAffectedOrNotFound(res)
}
```

#### Step 2: Add Repository to DI (`backend/deps.go`)

Add method to `deps` interface:
```go
GetBugReportRepo() *BugReportRepo
```

Add to `prodDeps.GetBugReportRepo()`:
```go
func (d *prodDeps) GetBugReportRepo() *BugReportRepo {
    return &BugReportRepo{db: d.db}
}
```

#### Step 3: Create Handlers (`backend/bug_reports_handler.go`)

```go
package handler

import (
    "encoding/json"
    "errors"
    "net/http"
)

type BugReportRequest struct {
    Title       string  `json:"title"`
    Description string  `json:"description"`
    StackTrace  *string `json:"stackTrace,omitempty"`
    UserAgent   *string `json:"userAgent,omitempty"`
    URL         *string `json:"url,omitempty"`
    Severity    string  `json:"severity"`
}

type BugReportListResponse struct {
    Reports []BugReport `json:"reports"`
}

func handleListBugReports(w http.ResponseWriter, r *http.Request) {
    userID, err := userIDFromRequest(r)
    if err != nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
        return
    }
    
    reports, err := serviceDeps.GetBugReportRepo().List(r.Context(), userID)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    if reports == nil {
        reports = []BugReport{}
    }
    writeJSON(w, http.StatusOK, BugReportListResponse{Reports: reports})
}

func handleCreateBugReport(w http.ResponseWriter, r *http.Request) {
    userID, err := userIDFromRequest(r)
    if err != nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
        return
    }
    
    var req BugReportRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" || req.Description == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and description are required"})
        return
    }
    
    if req.Severity == "" {
        req.Severity = "low"
    }
    
    report, err := serviceDeps.GetBugReportRepo().Create(r.Context(), userID, req.Title, req.Description,
        req.StackTrace, req.UserAgent, req.URL, req.Severity)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    writeJSON(w, http.StatusCreated, report)
}

func handleGetBugReport(w http.ResponseWriter, r *http.Request) {
    userID, err := userIDFromRequest(r)
    if err != nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
        return
    }
    
    id, ok := pathParam(r.URL.Path, "/bug-reports/")
    if !ok {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bug report id"})
        return
    }
    
    report, err := serviceDeps.GetBugReportRepo().Get(r.Context(), id, userID)
    if err != nil {
        if errors.Is(err, ErrNotFound) {
            writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug report not found"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    writeJSON(w, http.StatusOK, report)
}

func handleDeleteBugReport(w http.ResponseWriter, r *http.Request) {
    userID, err := userIDFromRequest(r)
    if err != nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
        return
    }
    
    id, ok := pathParam(r.URL.Path, "/bug-reports/")
    if !ok {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bug report id"})
        return
    }
    
    if err := serviceDeps.GetBugReportRepo().Delete(r.Context(), id, userID); err != nil {
        if errors.Is(err, ErrNotFound) {
            writeJSON(w, http.StatusNotFound, map[string]string{"error": "bug report not found"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
```

#### Step 4: Add Routes (`backend/handler.go`)

In the route switch in `Handle()`:
```go
case path == "bug-reports" && r.Method == http.MethodGet:
    authHandler(handleListBugReports).ServeHTTP(rec, r)
case path == "bug-reports" && r.Method == http.MethodPost:
    authHandler(handleCreateBugReport).ServeHTTP(rec, r)
case strings.HasPrefix(path, "bug-reports/") && r.Method == http.MethodGet:
    authHandler(handleGetBugReport).ServeHTTP(rec, r)
case strings.HasPrefix(path, "bug-reports/") && r.Method == http.MethodDelete:
    authHandler(handleDeleteBugReport).ServeHTTP(rec, r)
```

#### Step 5: Generate Types

Run `cd backend && make generate` to generate TypeScript types from Go structs. Commit the updated `frontend/src/api-types.gen.ts`.

### 6.4 Frontend Implementation

#### Step 1: Add API Calls (`frontend/src/api.ts`)

```typescript
import type { BugReport } from './api-types.gen'

export async function listBugReports(
    getToken: () => Promise<string | null>
): Promise<{ reports: BugReport[] }> {
    const token = await getToken()
    const resp = await fetch(`${apiUrl}/bug-reports`, {
        headers: { Authorization: `Bearer ${token}` },
    })
    const body = await resp.json()
    if (!resp.ok) throw new Error(body.error || 'Failed to list bug reports')
    return body
}

export async function createBugReport(
    data: {
        title: string
        description: string
        stackTrace?: string
        userAgent?: string
        url?: string
        severity?: string
    },
    getToken: () => Promise<string | null>
): Promise<BugReport> {
    const token = await getToken()
    const resp = await fetch(`${apiUrl}/bug-reports`, {
        method: 'POST',
        headers: {
            Authorization: `Bearer ${token}`,
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
    })
    const body = await resp.json()
    if (!resp.ok) throw new Error(body.error || 'Failed to submit bug report')
    return body
}

export async function deleteBugReport(
    id: number,
    getToken: () => Promise<string | null>
): Promise<void> {
    const token = await getToken()
    const resp = await fetch(`${apiUrl}/bug-reports/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
    })
    if (!resp.ok) {
        const body = await resp.json().catch(() => ({}))
        throw new Error(body.error || 'Failed to delete bug report')
    }
}
```

#### Step 2: Create Component (`frontend/src/components/BugReportForm.tsx`)

```typescript
import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { motion } from 'motion/react'
import { createBugReport, type BugReport } from '../api'

interface BugReportFormProps {
    onSubmitted?: (report: BugReport) => void
    onCancel?: () => void
}

export default function BugReportForm({ onSubmitted, onCancel }: BugReportFormProps) {
    const { getToken } = useAuth()
    const [title, setTitle] = useState('')
    const [description, setDescription] = useState('')
    const [severity, setSeverity] = useState<'low' | 'medium' | 'high' | 'critical'>('low')
    const [submitting, setSubmitting] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const textareaRef = useRef<HTMLTextAreaElement>(null)

    useEffect(() => {
        textareaRef.current?.focus()
    }, [])

    async function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        if (!title.trim() || !description.trim() || submitting) return

        setSubmitting(true)
        setError(null)
        try {
            const report = await createBugReport(
                {
                    title: title.trim(),
                    description: description.trim(),
                    severity,
                    url: window.location.pathname,
                    userAgent: navigator.userAgent,
                },
                getToken
            )
            onSubmitted?.(report)
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to submit bug report')
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <motion.div
            className="bug-report-form"
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.2 }}
        >
            <form onSubmit={handleSubmit}>
                <div className="form-group">
                    <label htmlFor="bug-title">Title</label>
                    <input
                        id="bug-title"
                        type="text"
                        value={title}
                        onChange={e => setTitle(e.target.value)}
                        placeholder="Brief summary of the issue"
                        disabled={submitting}
                        required
                    />
                </div>

                <div className="form-group">
                    <label htmlFor="bug-description">Description</label>
                    <textarea
                        ref={textareaRef}
                        id="bug-description"
                        value={description}
                        onChange={e => setDescription(e.target.value)}
                        placeholder="Detailed description of the issue"
                        disabled={submitting}
                        required
                        rows={4}
                    />
                </div>

                <div className="form-group">
                    <label htmlFor="bug-severity">Severity</label>
                    <select
                        id="bug-severity"
                        value={severity}
                        onChange={e => setSeverity(e.target.value as any)}
                        disabled={submitting}
                    >
                        <option value="low">Low</option>
                        <option value="medium">Medium</option>
                        <option value="high">High</option>
                        <option value="critical">Critical</option>
                    </select>
                </div>

                {error && <p className="form-error">{error}</p>}

                <div className="form-actions">
                    <button type="submit" disabled={submitting || !title.trim() || !description.trim()}>
                        {submitting ? 'Submitting…' : 'Submit Bug Report'}
                    </button>
                    {onCancel && (
                        <button type="button" className="btn-secondary" onClick={onCancel}>
                            Cancel
                        </button>
                    )}
                </div>
            </form>
        </motion.div>
    )
}
```

#### Step 3: Add to App Header or Menu

Option A: Add help menu with "Report a Bug" link:
```typescript
// In App.tsx
const [showBugReportForm, setShowBugReportForm] = useState(false)

return (
    <>
        {/* ... existing header ... */}
        <Show when="signed-in">
            <button 
                onClick={() => setShowBugReportForm(true)} 
                aria-label="Report a bug"
                title="Report a bug"
            >
                🐛
            </button>
        </Show>
        
        {showBugReportForm && (
            <BugReportForm 
                onSubmitted={() => {
                    setShowBugReportForm(false)
                    // Show success message
                }}
                onCancel={() => setShowBugReportForm(false)}
            />
        )}
    </>
)
```

### 6.5 Testing

#### Backend Tests (`backend/bug_reports_handler_test.go`)

```go
package handler

import (
    "context"
    "testing"
)

func TestBugReportCreate(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    
    repo := &BugReportRepo{db: db}
    
    br, err := repo.Create(context.Background(), "user123", "Title", "Description", nil, nil, nil, "high")
    if err != nil {
        t.Fatalf("Create failed: %v", err)
    }
    
    if br.Title != "Title" || br.UserID != "user123" {
        t.Errorf("Unexpected values: %+v", br)
    }
}

func TestBugReportList(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    
    repo := &BugReportRepo{db: db}
    repo.Create(context.Background(), "user123", "Bug 1", "Desc 1", nil, nil, nil, "low")
    repo.Create(context.Background(), "user123", "Bug 2", "Desc 2", nil, nil, nil, "high")
    
    reports, err := repo.List(context.Background(), "user123")
    if err != nil || len(reports) != 2 {
        t.Errorf("List failed: %v, len=%d", err, len(reports))
    }
}
```

#### Frontend Tests (`frontend/src/components/__tests__/BugReportForm.test.tsx`)

```typescript
import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import BugReportForm from '../BugReportForm'

vi.mock('../api', () => ({
    createBugReport: vi.fn().mockResolvedValue({
        id: 1,
        title: 'Test Bug',
        description: 'Test Description',
        severity: 'low',
        status: 'open',
        createdAt: '2026-05-15T00:00:00Z',
    }),
}))

describe('BugReportForm', () => {
    it('submits a bug report', async () => {
        const onSubmitted = vi.fn()
        render(<BugReportForm onSubmitted={onSubmitted} />)
        
        const titleInput = screen.getByPlaceholderText(/Brief summary/)
        const descInput = screen.getByPlaceholderText(/Detailed description/)
        
        await userEvent.type(titleInput, 'Bug Title')
        await userEvent.type(descInput, 'Bug Description')
        
        const submitBtn = screen.getByText(/Submit Bug Report/)
        await userEvent.click(submitBtn)
        
        await waitFor(() => expect(onSubmitted).toHaveBeenCalled())
    })
})
```

### 6.6 Migration & Deployment Checklist

1. **Create migration**: `backend/sql/005_bug_reports.sql`
2. **Create repository**: `backend/repo_bug_report.go`
3. **Add to DI**: Update `backend/deps.go`
4. **Create handlers**: `backend/bug_reports_handler.go`
5. **Add routes**: Update `backend/handler.go` route switch
6. **Generate types**: `cd backend && make generate && git add frontend/src/api-types.gen.ts`
7. **Add API functions**: `frontend/src/api.ts`
8. **Create component**: `frontend/src/components/BugReportForm.tsx`
9. **Integrate in UI**: Add button to `App.tsx` header
10. **Add tests**: Create handler and component tests
11. **Run lint**: `cd backend && make lint`
12. **Run tests**: `cd backend && make test` + `npm run test --prefix frontend`
13. **Commit**: Single commit with all changes
14. **Deploy**: `make build-backend build-frontend && make deploy`

---

## 7. IMPLEMENTATION CHECKLIST FOR BUG REPORTS

### Backend

- [ ] Create migration file: `backend/sql/005_bug_reports.sql`
- [ ] Create repository: `backend/repo_bug_report.go` (List, Create, Get, Delete, UpdateStatus)
- [ ] Update DI: Add `GetBugReportRepo()` to `deps` interface in `backend/deps.go`
- [ ] Create handlers: `backend/bug_reports_handler.go` (4 handlers)
- [ ] Add routes: Update switch in `backend/handler.go` (4 routes)
- [ ] Generate types: `cd backend && make generate`
- [ ] Add handler tests: `backend/bug_reports_handler_test.go`
- [ ] Run lint: `cd backend && make lint`
- [ ] Run tests: `cd backend && make test`

### Frontend

- [ ] Add API functions: `frontend/src/api.ts` (3 functions)
- [ ] Create component: `frontend/src/components/BugReportForm.tsx`
- [ ] Integrate: Add button + modal to `frontend/src/App.tsx`
- [ ] Add component tests: `frontend/src/components/__tests__/BugReportForm.test.tsx`
- [ ] Run tests: `npm run test --prefix frontend`

### General

- [ ] Commit all changes
- [ ] Test locally: `npm run dev`
- [ ] Run e2e tests: `npm run test:e2e`
- [ ] Deploy: `make build-backend build-frontend && make deploy`

---

## 8. KEY CONVENTIONS & PATTERNS

### Backend

1. **Routing**: String prefix matching + `pathParam()` utility
2. **Auth**: Clerk JWT via `clerkhttp.RequireHeaderAuthorization()` middleware
3. **Error handling**: Custom `ErrNotFound`, `ErrDuplicate` errors; standard `fmt.Errorf` wrapping
4. **Responses**: JSON via `writeJSON(w, status, response)`
5. **DI**: All services via `serviceDeps` interface (inject in tests)
6. **Repos**: `Repo*` type per table; CRUD methods; type-safe queries
7. **User isolation**: Verify ownership at handler level before CRUD
8. **Async jobs**: Generic `JobQueue[T]` with `MemQueue` implementation

### Frontend

1. **API calls**: Pure async functions taking `getToken` parameter
2. **Components**: Functional + hooks (useState, useEffect, useCallback)
3. **Error handling**: try/catch in event handlers; set error state; display inline
4. **Animations**: `motion` library for entrance/exit
5. **Forms**: Controlled inputs, disabled state while submitting
6. **Types**: Import from `api-types.gen.ts` (regenerated from Go)
7. **Design**: CSS variables from design system (--honey, --ink, etc.)

---

## 9. HELPFUL FILES TO UNDERSTAND

| File | Purpose |
|------|---------|
| `backend/handler.go` | HTTP router, middleware, core patterns |
| `backend/deps.go` | DI container interface |
| `backend/repo_class.go` | Example repository implementation |
| `backend/students.go` | Example handlers with CRUD pattern |
| `backend/notes.go` | Example interface usage (NoteCreator) |
| `backend/job_queue.go` | Generic async queue interface |
| `backend/job_queue_mem.go` | In-memory queue implementation |
| `frontend/src/api.ts` | All API call patterns |
| `frontend/src/App.tsx` | Root layout and component structure |
| `frontend/src/components/AddClassForm.tsx` | Example form component |
| `frontend/src/components/StudentDetail.tsx` | Example state management |
| `frontend/DESIGN.md` | Design system tokens and patterns |

---

## Summary

GradeBee is a well-architected, scalable codebase with:
- Clear separation of concerns (handler → repo → DB)
- Dependency injection for testability
- Generic patterns (job queues) for reusability
- Consistent error handling and API response patterns
- Strong type safety (TypeScript frontend, tygo-generated types)
- Proper user isolation via ownership checks

A bug report feature fits naturally into this architecture as an independent entity with its own repository, handlers, and frontend component. Follow the patterns established by classes/students/notes for consistency and maintainability.

