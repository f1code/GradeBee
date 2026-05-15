# GradeBee Architecture Diagram

## Overall System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        BROWSER / CLIENT                         │
│  React 19 + TypeScript                                          │
│  - Clerk Authentication                                         │
│  - Components (17 feature components)                           │
│  - Hooks (useAuth, useDrivePicker, useMediaQuery)              │
└────────────────────────┬────────────────────────────────────────┘
                         │
                    HTTPS / REST
                    Bearer Token (JWT)
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│                   GO BACKEND (net/http)                         │
│  handler.go: Single entry point Handle(w, r)                  │
│                                                                 │
│  Route Matching → Auth Middleware → Handler → Response         │
│                                                                 │
│  38 Endpoints:                                                  │
│  - Classes (4), Students (4), Notes (5), Reports (5)           │
│  - Voice Notes (7), Report Examples (4), etc.                  │
└────────────────────────┬────────────────────────────────────────┘
                         │
        ┌────────────────┼────────────────┐
        │                │                │
        ▼                ▼                ▼
   ┌─────────┐    ┌────────────┐   ┌─────────────┐
   │ Repos   │    │ Services   │   │  External   │
   ├─────────┤    ├────────────┤   │  Services   │
   │ Class   │    │ Transcriber│   │ ┌─────────┐ │
   │ Student │    │ Extractor  │   │ │ Whisper │ │
   │ Note    │    │ NoteCreator│   │ │ Claude  │ │
   │ Report  │    │ Generator  │   │ │ Clerk   │ │
   │ VoiceNote│   │ JobQueue[T]│   │ └─────────┘ │
   │ BugReport│   └────────────┘   └─────────────┘
   └─────────┘
        │
        ▼
   ┌─────────────────────┐
   │  SQLite Database    │
   │  (WAL mode)         │
   ├─────────────────────┤
   │ classes             │
   │ students            │
   │ notes               │
   │ reports             │
   │ report_examples     │
   │ voice_notes         │
   │ report_example_cls  │
   │ bug_reports         │
   └─────────────────────┘
```

## Request Flow Deep Dive

```
1. Client Request
   └─ HTTP GET/POST/PUT/DELETE /endpoint
   └─ Header: Authorization: Bearer <JWT>
   └─ Body (optional): JSON

2. handler.go::Handle()
   ├─ Create request-scoped logger
   ├─ Extract path from URL
   ├─ Match path against routes using strings.HasPrefix
   ├─ Find matching handler
   └─ Wrap with authHandler middleware

3. authHandler middleware
   ├─ Extract JWT from Authorization header
   ├─ Verify JWT signature with Clerk
   ├─ Extract userID from claims
   ├─ Add to request context
   └─ Call next handler

4. Handler (e.g., handleCreateClass)
   ├─ Get userID from request context
   ├─ Decode JSON request body
   ├─ Validate input fields
   ├─ Call serviceDeps.GetClassRepo().Create(userID, name, group)
   └─ Check for errors:
       ├─ ErrDuplicate → 409 Conflict
       ├─ Other error → 500 Internal Server Error
       └─ Success → 201 Created

5. ClassRepo.Create()
   ├─ Execute SQL INSERT
   ├─ Handle SQLite constraint violations
   ├─ RETURNING clause to get created row
   └─ Return Class struct or error

6. writeJSON response
   ├─ Marshal struct to JSON
   ├─ Set Content-Type: application/json
   ├─ Write status code
   ├─ Write JSON body
   └─ Log request metrics

7. Client receives response
   ├─ Status code + headers
   ├─ JSON body: {data} or {error: "msg"}
   └─ Error handling in component
```

## Database Schema & Relationships

```
┌──────────────────────────────────────────────────────┐
│  User (Clerk JWT, no DB row)                        │
│  └─ user_id (extracted from JWT)                    │
└──────────────┬───────────────────────────────────────┘
               │
               │ FK: user_id
               ▼
┌──────────────────────────────────────────────────────┐
│  classes                                             │
│  ├─ id (PK)                                          │
│  ├─ user_id (FK)                    ◄─ User         │
│  ├─ name, class_name, group_name                    │
│  ├─ position (order)                                │
│  └─ created_at                                      │
│  UNIQUE(user_id, class_name, group_name)            │
└──────────────┬───────────────────────────────────────┘
               │
        ┌──────┼──────┐
        │ FK   │ FK   │
        ▼      ▼      ▼
   ┌─────────────────┐    ┌──────────────────┐
   │  students       │    │ report_examples  │
   ├─────────────────┤    ├──────────────────┤
   │ id (PK)         │    │ id (PK)          │
   │ class_id (FK)   │    │ user_id (FK)     │
   │ name            │    │ name, content    │
   │ created_at      │    │ status, file_path│
   │ UNIQUE(class_id,│    │ created_at       │
   │        name)    │    └────────┬─────────┘
   └─────────┬───────┘             │
             │                     │ M-M
        ┌────┴──────┐       ┌──────▼──────────┐
        │ FK         │       │ report_example  │
        │ class_id   │       │ _classes        │
        ▼            ▼       ├─────────────────┤
   ┌──────────┐  ┌────────┐ │ example_id (FK) │
   │  notes   │  │reports │ │ class_name      │
   ├──────────┤  ├────────┤ │ PK(example_id,  │
   │ id (PK)  │  │ id(PK) │ │    class_name)  │
   │ student_ │  │student_│ └─────────────────┘
   │ id (FK)  │  │ id(FK) │
   │ date,    │  │start_  │
   │ summary, │  │date,   │
   │transcript│  │end_    │
   │ source   │  │date,   │
   │ created_ │  │html    │
   │ at       │  │created_│
   └──────────┘  │at      │
                 └────────┘

┌─────────────────────────────────────────────────────┐
│  voice_notes (file tracking)                       │
│  ├─ id (PK)                                        │
│  ├─ user_id (FK)                    ◄─ User        │
│  ├─ file_name, file_path                          │
│  ├─ processed_at (NULL = queued, timestamp = done) │
│  └─ created_at                                     │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  bug_reports (NEW FEATURE)                         │
│  ├─ id (PK)                                        │
│  ├─ user_id (FK)                    ◄─ User        │
│  ├─ title, description                            │
│  ├─ stack_trace, user_agent, url                  │
│  ├─ status (open/triaged/fixed/closed)            │
│  ├─ severity (low/medium/high/critical)           │
│  ├─ created_at, updated_at                        │
│  └─ Indexes: (user_id), (status), (created_at)    │
└─────────────────────────────────────────────────────┘
```

## Dependency Injection Container

```
serviceDeps (package-level singleton)
    ↓
prodDeps struct implements deps interface
    ├─ db *sql.DB
    ├─ uploadsDir string
    └─ Methods (26 total):
        │
        ├─ Repo Methods
        │  ├─ GetClassRepo() → &ClassRepo{db}
        │  ├─ GetStudentRepo() → &StudentRepo{db}
        │  ├─ GetNoteRepo() → &NoteRepo{db}
        │  ├─ GetReportRepo() → &ReportRepo{db}
        │  ├─ GetBugReportRepo() → &BugReportRepo{db}
        │  ├─ GetVoiceNoteRepo() → &VoiceNoteRepo{db}
        │  ├─ GetExampleRepo() → &ReportExampleRepo{db}
        │  └─ GetReportExampleClassesRepo() → &ReportExampleClassesRepo{db}
        │
        ├─ Service Methods
        │  ├─ GetTranscriber() → &whisperTranscriber
        │  ├─ GetExtractor() → &gptExtractor
        │  ├─ GetNoteCreator() → &dbNoteCreator
        │  ├─ GetReportGenerator() → &gptReportGenerator
        │  ├─ GetExampleExtractor() → &gptExampleExtractor
        │  ├─ GetRoster() → &dbRoster
        │  └─ GetDriveClient() → *drive.Service
        │
        ├─ Queue Methods
        │  ├─ GetVoiceNoteQueue() → MemQueue[VoiceNoteJob]
        │  └─ GetExtractionQueue() → MemQueue[ExtractionJob]
        │
        └─ Infra Methods
           ├─ GetDB() → *sql.DB
           └─ GetUploadsDir() → string

Test Override:
    serviceDeps = &testDeps{
        mockRepo: stubRepo{...},
        mockTranscriber: mockTranscriber{...},
        // Override with test doubles
    }
```

## Frontend Component Hierarchy

```
App.tsx (root)
├─ Auth Gate (Clerk)
├─ Header
│  ├─ Logo + Title
│  ├─ UserButton (if signed in)
│  └─ How-It-Works button (if signed in)
└─ Tab Router: notes / reports
   │
   ├─ TAB: "notes"
   │  └─ StudentList
   │     ├─ ClassGroup (each class)
   │     │  └─ StudentItem (each student)
   │     │     └─ StudentDetail (on click)
   │     │        ├─ NotesList
   │     │        │  ├─ NoteItem
   │     │        │  └─ NoteEditor (edit/create)
   │     │        └─ ReportHistory
   │     │
   │     ├─ AddClassForm (modal)
   │     ├─ AddStudentForm (modal)
   │     ├─ AudioUpload (drop zone)
   │     │  ├─ drag-drop file upload
   │     │  └─ JobStatus (progress indicator)
   │     ├─ TranscriptReview (display extracted notes)
   │     ├─ TextNotes (paste notes form)
   │     └─ BugReportForm (modal) ◄─ NEW
   │
   └─ TAB: "reports"
      └─ ReportGeneration
         ├─ DateRange picker
         ├─ StudentSelector
         ├─ Generate button
         └─ ReportViewer (HTML display)
         └─ ReportExamples (style matching)
            ├─ Upload example (PDF/image)
            └─ ExampleList
```

## Async Job Processing Pipeline

```
User Action
    ↓
1. Upload Voice Note / Submit Text / Upload Example PDF
    ├─ Save file to disk: /data/uploads/user_id_timestamp_filename
    ├─ Create DB row (voice_notes / report_examples with status='processing')
    ├─ Publish job to MemQueue
    └─ Return immediately with uploadId
    ▼
2. Frontend Polls GET /voice-notes/jobs
    ├─ Every 500ms-1s
    ├─ Display status ("transcribing", "extracting", "creating_notes")
    ├─ Stop polling when status="done" or "failed"
    └─ Show error on failure
    ▼
3. Backend Worker Goroutine (picks job from queue)
    ├─ VoiceNote pipeline:
    │  ├─ Step 1: Transcribe (Whisper API)
    │  ├─ Step 2: Extract (GPT on transcript)
    │  └─ Step 3: Create Notes (NoteCreator for each student)
    │
    ├─ Example PDF pipeline:
    │  ├─ Step 1: Convert PDF → JPEG (pdftoppm)
    │  ├─ Step 2: Extract text (GPT Vision per page)
    │  └─ Step 3: Save content to DB
    │
    └─ Update DB row: status='done' or status='failed'
    ▼
4. Frontend Receives Completion
    ├─ Auto-refresh UI with new notes/examples
    └─ Show success/error message
```

## Error Handling Flow

```
HTTP Handler
    ├─ Input validation error
    │  └─ writeJSON(400, {error: "field is required"})
    │
    ├─ User not authorized
    │  └─ authHandler returns 401/403
    │
    ├─ Resource not found
    │  ├─ errors.Is(err, ErrNotFound)
    │  └─ writeJSON(404, {error: "not found"})
    │
    ├─ Duplicate constraint
    │  ├─ errors.Is(err, ErrDuplicate)
    │  └─ writeJSON(409, {error: "already exists"})
    │
    └─ Unexpected error
       ├─ Log full error with slog
       └─ writeJSON(500, {error: "internal server error"})

Frontend
    ├─ API call throws error
    │  ├─ Catch: setError(err.message)
    │  ├─ Finally: setSubmitting(false)
    │  └─ Render: {error && <p>{error}</p>}
    │
    └─ Display in UI
       └─ User can retry or dismiss
```

## Type Generation Pipeline

```
Go Backend
    ├─ Struct with json tags
    │  type ClassWithCount struct {
    │      Class `tstype:",extends"` // Flattens
    │      StudentCount int `json:"studentCount"`
    │  }
    │
    └─ tygo.yaml config
       └─ Specifies type_mappings (time.Time → string, etc.)
           ▼
    tygo generate
        ├─ Parse Go AST
        ├─ Extract structs with json tags
        ├─ Apply type mappings
        ├─ Generate TypeScript interfaces
        └─ Write frontend/src/api-types.gen.ts
            ▼
React Frontend
    └─ Import types
       import type { ClassWithCount } from './api-types.gen'
       └─ Use in components & API layer
           └─ Full type safety across frontend
```

## Deployment & Infrastructure

```
Development
    npm run dev
    ├─ Backend: cd backend/cmd/server && go run .
    ├─ Frontend: cd frontend && npm run dev
    └─ Concurrently on localhost:8080 + localhost:5173
           ▼
Testing
    npm run test            # Frontend + backend tests
    npm run test:e2e        # Playwright E2E tests
    cd backend && make lint # Go linter
           ▼
Production Build
    make build-backend      # GOOS=linux CGO_ENABLED=0 go build -o dist/gradebee
    make build-frontend     # npm run --prefix frontend build
           ▼
Docker Deployment
    Dockerfile
        ├─ Build stage: Go binary
        ├─ Build stage: React SPA
        └─ Runtime: Alpine Linux with Caddy reverse proxy
            ▼
    docker-compose.yml
        ├─ Service: backend (Go binary)
        ├─ Service: frontend (Caddy serving dist/)
        └─ Volume: /data/gradebee.db (persistent SQLite)
            ▼
VPS Deployment
    Scaleway VM + Docker
    ├─ Caddyfile: HTTPS, reverse proxy, gzip
    ├─ SQLite: /data/gradebee.db (backed up to S3)
    ├─ Uploads: /data/uploads/ (7-day cleanup)
    └─ Restart: docker compose up -d --build
```

This architecture enables:
- Easy testing (DI with test doubles)
- Clear separation of concerns
- Simple addition of new features (follow CRUD patterns)
- Scalability (job queue for async work)
- Type safety (generated types)
- User isolation (ownership checks throughout)
