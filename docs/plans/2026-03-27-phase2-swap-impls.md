# Phase 2: Backend — Swap Implementations

## Goal

Replace all Google Drive/Sheets/Docs-backed handlers and service implementations with DB-backed equivalents. After this phase, the backend reads/writes classes, students, notes, reports, and examples from SQLite via the repos created in Phase 1. Google APIs are reduced to Drive-read-only (for `/drive-import` file downloads). Audio files are saved to local disk instead of Drive.

**Prerequisite:** Phase 1 complete (SQLite, migrations, all `Repo*` types).

---

## File-by-File Changes

### 1. `backend/deps.go` — Rewrite DI container

**Remove:**
- `GoogleServices(r *http.Request) (*googleServices, error)`
- `GoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error)`
- `GetDriveStore(svc *googleServices) DriveStore`
- `GetGradeBeeMetadata(ctx context.Context, userID string) (*gradeBeeMetadata, error)`
- All `prodDeps` methods that reference `googleServices`, `DriveStore`, or `gradeBeeMetadata`

**Remove parameters from existing methods** (drop `svc *googleServices`):
- `GetRoster(ctx, svc)` → `GetRoster(ctx, userID)`
- `GetNoteCreator(svc)` → `GetNoteCreator()`
- `GetExampleStore(svc)` → `GetExampleStore()`
- `GetReportGenerator(svc)` → `GetReportGenerator()`

**Add:**
- `GetDB() *sql.DB` — returns the SQLite connection
- `GetClassRepo() *ClassRepo`
- `GetStudentRepo() *StudentRepo`
- `GetNoteRepo() *NoteRepo`
- `GetReportRepo() *ReportRepo`
- `GetExampleRepo() *ReportExampleRepo`
- `GetUploadRepo() *UploadRepo`
- `GetDriveClient(ctx context.Context, userID string) (*drive.Service, error)` — Drive-read-only client for `/drive-import`

**Keep unchanged:**
- `GetTranscriber() (Transcriber, error)`
- `GetExtractor() (Extractor, error)`
- `GetExampleExtractor() (ExampleExtractor, error)`
- `GetUploadQueue() (UploadQueue, error)`

**`prodDeps` struct changes:** Add fields for `*sql.DB` and all repos (initialized at startup in `main.go` and passed in). The struct becomes:

```
type prodDeps struct {
    db          *sql.DB
    classRepo   *ClassRepo
    studentRepo *StudentRepo
    noteRepo    *NoteRepo
    reportRepo  *ReportRepo
    exampleRepo *ReportExampleRepo
    uploadRepo  *UploadRepo
    uploadsDir  string  // e.g. "/data/uploads"
}
```

**`prodDeps` method implementations:**
- `GetRoster(ctx, userID)` → return `newDBRoster(p.classRepo, p.studentRepo, userID)`
- `GetNoteCreator()` → return `newDBNoteCreator(p.noteRepo)`
- `GetExampleStore()` → return `newDBExampleStore(p.exampleRepo)`
- `GetReportGenerator()` → return `newDBReportGenerator(p.noteRepo, p.reportRepo, p.exampleRepo)` (no Drive/Docs)
- `GetDriveClient(ctx, userID)` → call `getGoogleOAuthToken`, create Drive-only service

**Keep:** `InitUploadQueue`, `ServiceDeps()`, `serviceDeps` variable pattern — same testability approach.

---

### 2. `backend/handler.go` — New routes, remove old

**Remove routes:**
- `GET /setup`, `POST /setup`
- Remove `getSetupHandler`, `setupHandler` vars

**Remove handler vars** that will be rewritten (they'll be re-declared with new implementations):
- (none removed, but many are rewritten in-place)

**Add new routes** (in the `switch` block). Use path parameter extraction helper (see below):

| Method | Path | Handler func |
|--------|------|-------------|
| GET | `classes` | `handleListClasses` |
| POST | `classes` | `handleCreateClass` |
| PUT | `classes/{id}` | `handleUpdateClass` |
| DELETE | `classes/{id}` | `handleDeleteClass` |
| GET | `classes/{id}/students` | `handleListStudents` |
| POST | `classes/{id}/students` | `handleCreateStudent` |
| PUT | `students/{id}` | `handleUpdateStudent` |
| DELETE | `students/{id}` | `handleDeleteStudent` |
| GET | `students/{id}/notes` | `handleListNotes` |
| POST | `students/{id}/notes` | `handleCreateNote` |
| GET | `notes/{id}` | `handleGetNote` |
| PUT | `notes/{id}` | `handleUpdateNote` |
| DELETE | `notes/{id}` | `handleDeleteNote` |
| POST | `reports` | `handleGenerateReports` (rewritten) |
| POST | `reports/{id}/regenerate` | `handleRegenerateReport` (rewritten) |
| GET | `students/{id}/reports` | `handleListReports` |
| GET | `reports/{id}` | `handleGetReport` |
| DELETE | `reports/{id}` | `handleDeleteReport` |

**Keep unchanged:**
- `GET /health`
- `GET /google-token` — still needed for Drive Picker
- `POST /drive-import` — rewritten but same path
- `GET /jobs`, `POST /jobs/retry`, `POST /jobs/dismiss`
- `GET /report-examples`, `POST /report-examples`, `DELETE /report-examples`
- `POST /upload`

**Add path param helper:** A small `pathParam(path, prefix)` function that extracts `{id}` from paths like `classes/42/students`. The current routing uses `strings.TrimPrefix` + `switch`; extend to match patterns with path segments. Alternatively, switch to a lightweight function that parses `path` segments. Example:

```go
// matchRoute returns (pattern, params) for paths like "classes/42/students"
// Pattern: "classes/*/students", params: ["42"]
```

Keep the existing `switch`-based router (no external router dep). Add cases using `strings.HasPrefix` + segment splitting for parameterized routes.

**Add `Access-Control-Allow-Methods`:** Include `PUT` in the CORS header (currently only `GET, POST, DELETE, OPTIONS`).

**UserID extraction:** Currently handlers get userID via `svc.User.UserID` after calling `GoogleServices(r)`. Replace with a shared helper:

```go
func userIDFromRequest(r *http.Request) (string, error)
```

This extracts `claims.Subject` from Clerk session claims (same as `newGoogleServices` does today, but without building Google clients).

---

### 3. `backend/roster.go` — New `dbRoster`

**Remove:** `sheetsRoster` struct, `newSheetsRoster`, all its methods.

**Keep:** `Roster` interface (same signatures), `classGroup` and `student` types (already in `students.go`).

**Add:** `dbRoster` struct:

```go
type dbRoster struct {
    classRepo   *ClassRepo
    studentRepo *StudentRepo
    userID      string
}

func newDBRoster(cr *ClassRepo, sr *StudentRepo, userID string) *dbRoster
```

Methods:
- `ClassNames(ctx) ([]string, error)` — `classRepo.ListByUser(ctx, userID)`, extract names
- `Students(ctx) ([]classGroup, error)` — `classRepo.ListByUser` + `studentRepo.ListByClass` for each, build `[]classGroup`
- `SpreadsheetURL() string` — return `""` (no longer relevant; callers that used this are being removed)

---

### 4. `backend/students.go` — Rewrite handler

**Remove:** Current `handleGetStudents` implementation (calls `GoogleServices`, `GetRoster`).

**Rewrite `handleGetStudents`:** Extract `userID` via `userIDFromRequest`. Call `serviceDeps.GetRoster(ctx, userID).Students(ctx)`. Return new response shape:

```go
type studentsResponse struct {
    Classes []classGroupResponse `json:"classes"`
}
type classGroupResponse struct {
    ID       int64             `json:"id"`
    Name     string            `json:"name"`
    Students []studentResponse `json:"students"`
}
type studentResponse struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}
```

**Remove:** `SpreadsheetURL` from response. Update `classGroup`/`student` types to include IDs (or create new response types).

**Keep:** `parseStudentRows` can be deleted (no longer parsing spreadsheet data).

**Add new handler functions** (can live in this file or a new `classes_handler.go`):
- `handleListClasses` — list classes for user (with student counts)
- `handleCreateClass` — JSON body `{name}`, calls `classRepo.Create`
- `handleUpdateClass` — JSON body `{name}`, calls `classRepo.Rename`
- `handleDeleteClass` — calls `classRepo.Delete` (cascade handled by FK)
- `handleListStudents` — `studentRepo.ListByClass(classID)`, verify class belongs to user
- `handleCreateStudent` — JSON body `{name}`, verify class ownership, `studentRepo.Create`
- `handleUpdateStudent` — JSON body `{name, classId?}`, `studentRepo.Update`
- `handleDeleteStudent` — verify ownership, `studentRepo.Delete`

All handlers follow existing pattern: extract userID, call repo, return JSON. Authorization: verify the class/student belongs to the authenticated user before mutation.

---

### 5. `backend/notes.go` — New `dbNoteCreator`

**Remove:** `driveNoteCreator` struct, `newDriveNoteCreator`, `populateDoc`, all Google Docs imports.

**Keep:** `NoteCreator` interface, `CreateNoteRequest`, `CreateNoteResponse`.

**Modify `CreateNoteRequest`:** Replace `NotesRootID string` with `StudentID int64`. Remove `ClassName` (student already belongs to a class in DB).

**Modify `CreateNoteResponse`:** Replace `DocID`/`DocURL` with `NoteID int64`.

**Add:** `dbNoteCreator` struct:

```go
type dbNoteCreator struct {
    noteRepo *NoteRepo
}
func newDBNoteCreator(nr *NoteRepo) *dbNoteCreator
```

`CreateNote(ctx, req)`:
- Call `noteRepo.Create(ctx, NoteRow{StudentID: req.StudentID, Date: req.Date, Summary: req.Summary, Transcript: req.Transcript, Source: "auto"})`
- Return `&CreateNoteResponse{NoteID: id}`

**Add note CRUD handlers** (in this file or `notes_handler.go`):
- `handleListNotes` — `GET /students/:id/notes` → `noteRepo.ListByStudent(ctx, studentID)`, verify student ownership
- `handleGetNote` — `GET /notes/:id` → `noteRepo.Get(ctx, id)`, verify ownership
- `handleCreateNote` — `POST /students/:id/notes` → JSON `{date, summary}`, create with `source: "manual"`, no transcript
- `handleUpdateNote` — `PUT /notes/:id` → JSON `{summary}`, `noteRepo.Update`
- `handleDeleteNote` — `DELETE /notes/:id` → `noteRepo.Delete`

---

### 6. `backend/metadata_index.go` — DELETE

**Delete entirely.** The `MetadataIndex` interface, `driveMetadataIndex`, `IndexEntry`, `StudentIndex`, `findDriveFolder`, `findOrCreateDriveFolder` — all removed. Notes are directly queryable via `noteRepo`. The report generator uses `noteRepo.ListByStudentDateRange` directly. No replacement file needed.

---

### 7. `backend/report_generator.go` — Return HTML, drop Drive/Docs

**Remove:** All Google Drive/Docs imports and operations: `populateReportDoc`, `replaceReportDoc`, `readFeedback`, `findExistingReport`, folder creation. Remove `drive` and `docs` fields from struct.

**Modify structs:**

`GenerateReportRequest`:
- Remove `NotesRootID`, `ReportsID`, `ExamplesFolderID` (Drive folder IDs)
- Add `StudentID int64` (to query notes from DB)
- Add `UserID string` (to load examples)
- Keep `Student`, `Class`, `StartDate`, `EndDate`, `Instructions`

`GenerateReportResponse`:
- Remove `DocID`, `DocURL`, `Skipped`
- Add `ReportID int64`, `HTML string`

`RegenerateReportRequest`:
- Remove `DocID`, `NotesRootID`, `ExamplesFolderID`
- Add `ReportID int64`, `Feedback string` (feedback now comes from frontend, not from a Google Doc section)
- Keep `Student`, `Class`, `StartDate`, `EndDate`, `Instructions`

**Rewrite `gptReportGenerator`:**

```go
type gptReportGenerator struct {
    client      *openai.Client
    noteRepo    *NoteRepo
    reportRepo  *ReportRepo
    exampleRepo *ReportExampleRepo
}
func newDBReportGenerator(nr *NoteRepo, rr *ReportRepo, er *ReportExampleRepo) (*gptReportGenerator, error)
```

`Generate(ctx, req)`:
1. Query notes: `noteRepo.ListByStudentDateRange(ctx, req.StudentID, req.StartDate, req.EndDate)`
2. Load examples: `exampleRepo.ListByUser(ctx, req.UserID)`
3. Build prompt via `buildReportPrompt` (keep `report_prompt.go` unchanged — adjust input types as needed)
4. Call GPT → get HTML string (update system prompt to request HTML output instead of plain text)
5. Save: `reportRepo.Create(ctx, ReportRow{StudentID, StartDate, EndDate, HTML, Instructions})`
6. Return `&GenerateReportResponse{ReportID: id, HTML: html}`

`Regenerate(ctx, req)`:
1. Load existing report: `reportRepo.Get(ctx, req.ReportID)` — to get the original HTML
2. Query notes (same as Generate)
3. Load examples
4. Build prompt with `req.Feedback` (previously read from Google Doc "Teacher Feedback" section — now passed explicitly from frontend)
5. Call GPT → new HTML
6. Update: `reportRepo.Update(ctx, req.ReportID, newHTML)` (or create a new report row — TBD)
7. Return response

**Remove:** `readFeedback` (feedback comes from request body now).

---

### 8. `backend/reports_handler.go` — Rewrite handlers

**Rewrite `handleGenerateReports`:**
- Extract `userID` via `userIDFromRequest` (not `GoogleServices`)
- No longer needs `getGradeBeeMetadata` — pass `StudentID` + `UserID` to generator
- Request body changes: `students` array should include `studentId` (int64) instead of just name/class
- Response changes: return `reportId` + `html` instead of `docId`/`docUrl`

**Rewrite `handleRegenerateReport`:**
- Request body: `{reportId, feedback, studentId, startDate, endDate, instructions}`
- No longer reads feedback from Google Docs

**Add new handlers:**
- `handleListReports` — `GET /students/:id/reports` → `reportRepo.ListByStudent`
- `handleGetReport` — `GET /reports/:id` → `reportRepo.Get`, return HTML
- `handleDeleteReport` — `DELETE /reports/:id` → `reportRepo.Delete`

---

### 9. `backend/report_examples.go` — New `dbExampleStore`

**Remove:** `driveExampleStore`, `newDriveExampleStore`, all Drive imports.

**Keep:** `ExampleStore` interface, `ReportExample` struct.

**Modify interface** — replace Drive folder IDs with userID:
- `ListExamples(ctx, userID string)` instead of `(ctx, examplesFolderID string)`
- `UploadExample(ctx, userID, name, content string)` instead of `(ctx, examplesFolderID, ...)`
- `ReadExample(ctx, exampleID int64)` — use int64 ID
- `DeleteExample(ctx, exampleID int64)`

**Add:** `dbExampleStore`:

```go
type dbExampleStore struct {
    repo *ReportExampleRepo
}
func newDBExampleStore(r *ReportExampleRepo) *dbExampleStore
```

Delegates directly to repo methods.

---

### 10. `backend/report_examples_handler.go` — Rewrite handlers

**Remove:** All `GoogleServices` calls, `getGradeBeeMetadata` calls, `ensureReportExamplesFolder`.

**Rewrite all three handlers** to:
- Extract `userID` via `userIDFromRequest`
- Get store via `serviceDeps.GetExampleStore()`
- Pass `userID` instead of folder IDs
- Delete handler: parse int64 ID from request body

The multipart upload + GPT Vision extraction flow stays the same — only the storage backend changes.

---

### 11. `backend/upload.go` — Save to disk + `uploads` table

**Remove:** All `GoogleServices`, `DriveStore`, `getGradeBeeMetadata` calls.

**Rewrite `handleUpload`:**
1. Parse multipart form (keep size limit, MIME validation — unchanged)
2. Extract `userID` via `userIDFromRequest`
3. Generate a unique filename: `{uuid}{ext}` (use `audio_format.go` for extension)
4. Write file to `uploadsDir/{filename}` (get `uploadsDir` from deps or env)
5. Insert row in `uploads` table via `uploadRepo.Create(ctx, UploadRow{UserID, FileName: header.Filename, FilePath: diskPath})`
6. Dispatch `UploadJob` to queue — use `upload.ID` (int64) as the job identifier instead of Drive file ID
7. Return `{uploadId, fileName}`

**Impact on `UploadJob` type** (`upload_queue.go`): Change `FileID string` to `UploadID int64` and `FilePath string`. Update `kvKey` accordingly. This ripples to `mem_queue.go`, `jobs_list.go`, `jobs_retry.go`, `jobs_dismiss.go`.

---

### 12. `backend/upload_process.go` — Use DB-backed deps

**Remove:** `GoogleServicesForUser` call, `GetDriveStore` usage, `GetGradeBeeMetadata` call.

**Rewrite `processUploadJob`:**

Step 1 (Transcribe):
- Read audio from local disk (`job.FilePath`) instead of `store.Download(ctx, fileID)`
- Whisper prompt: get class names from `dbRoster` via `d.GetRoster(ctx, job.UserID)`
- Keep transcriber call unchanged

Step 2 (Extract):
- Get students from `dbRoster` (same as above, reuse)
- Keep extractor call unchanged

Step 3 (Create Notes):
- Get `noteCreator` via `d.GetNoteCreator()` (no `svc` param)
- For each extracted student: need to resolve `StudentID` from name+class. Add a lookup: `studentRepo.FindByNameAndClass(ctx, name, className, userID) → (int64, error)`. Skip if student not found in DB (log warning).
- `CreateNoteRequest` now uses `StudentID int64` instead of `NotesRootID`+folder paths
- No `MetadataIndex.AppendEntry` needed (note is directly in DB)

Step 4 (Done):
- Mark upload as processed: `uploadRepo.MarkProcessed(ctx, job.UploadID)`
- Update job status to done (keep existing queue update)
- `NoteLinks` in job: change from `{Name, URL}` to `{Name, NoteID}` (frontend will link to in-app note view)

---

### 13. `backend/upload_cleanup.go` — NEW

**Purpose:** Background goroutine that deletes processed audio files from disk and their `uploads` rows after a retention period.

**Functions:**

```go
func StartUploadCleanup(ctx context.Context, repo *UploadRepo, uploadsDir string, retention time.Duration, interval time.Duration)
```

- Runs in a goroutine, ticks every `interval` (default 1 hour)
- Calls `repo.ListStale(ctx, retention)` → rows where `processed_at < now - retention`
- For each: `os.Remove(filePath)`, then `repo.Delete(ctx, id)`
- Logs errors but continues
- Stops on `ctx.Done()`

**Startup:** Called from `cmd/server/main.go` with `ctx` from signal handler. Default retention: 7 days (`UPLOAD_RETENTION_HOURS` env var).

---

### 14. `backend/drive_import.go` — Download to local disk

**Remove:** `DriveStore` usage (`store.Copy`, `store.GetMimeType`), `getGradeBeeMetadata` call.

**Rewrite `handleDriveImport`:**
1. Parse request (keep `fileId`, `fileName` — unchanged)
2. Extract `userID` via `userIDFromRequest`
3. Get Drive read client: `driveSvc, err := serviceDeps.GetDriveClient(ctx, userID)`
4. Validate file: `driveSvc.Files.Get(req.FileID).Fields("mimeType").Do()` → check `isAllowedAudioType`
5. Download file: `driveSvc.Files.Get(req.FileID).Download()` → stream to local disk `{uploadsDir}/{uuid}{ext}`
6. Insert `uploads` row via `uploadRepo.Create`
7. Dispatch `UploadJob` (same as rewritten `upload.go`)
8. Return `{uploadId, fileName}`

---

### 15. `backend/google.go` — Slim down to Drive-read-only

**Remove:**
- `googleServices` struct (or reduce to just `*drive.Service`)
- `newGoogleServices(r)` — no longer needed (handlers don't need full Google client)
- `newGoogleServicesForUser(ctx, userID)` — no longer needed
- `sheets.NewService`, `docs.NewService` calls
- All Sheets/Docs imports

**Keep:**
- `apiError` struct + `writeAPIError` helper (used throughout)
- `createFolder` — **delete** (no longer creating Drive folders)

**Add:** A simpler function for Drive-read-only client construction:

```go
func newDriveReadClient(ctx context.Context, userID string) (*drive.Service, error)
```

Calls `getGoogleOAuthToken(ctx, userID)` → builds `drive.Service` with token. Used only by `drive_import.go` (via `deps.GetDriveClient`).

---

### 16. `backend/auth.go` — Keep as-is

No changes. Still needed for:
- `getGoogleOAuthToken` — used by `google_token.go` (Drive Picker) and `newDriveReadClient`
- `clerkUser` type — may simplify or remove if handlers extract userID directly from claims

---

### 17. `backend/jobs_dismiss.go` — Update for `processed_at`

**Add:** When dismissing a job, also mark `processed_at` on the corresponding `uploads` row so the file enters the cleanup window:

```go
uploadRepo.MarkProcessed(ctx, job.UploadID)
```

This ensures dismissed failed-job files get cleaned up.

---

### 18. Files to DELETE

| File | Reason |
|------|--------|
| `backend/clerk_metadata.go` | `gradeBeeMetadata` (Drive folder IDs in Clerk) no longer needed |
| `backend/setup.go` | No Drive workspace provisioning |
| `backend/drive_store.go` | `DriveStore` interface + `sheetsDriveStore` replaced by local disk |
| `backend/metadata_index.go` | `MetadataIndex` replaced by direct `noteRepo` queries |

---

### 19. `backend/cmd/server/main.go` — Startup changes

**Add:**
- Open SQLite DB, run migrations (from Phase 1)
- Initialize repos, create `prodDeps` with repos + DB
- Create uploads directory if not exists
- Start `StartUploadCleanup` goroutine
- Pass new `prodDeps` to `InitUploadQueue`

**Remove:**
- No Google-specific init needed at startup

---

### 20. `backend/upload_queue.go` + `backend/mem_queue.go` — Update job type

**Modify `UploadJob`:**
- `FileID string` → `UploadID int64`
- Add `FilePath string` (local disk path)
- `kvKey` uses `fmt.Sprintf("%s/%d", userID, uploadID)`

**Modify `NoteLink`:**
- `URL string` → `NoteID int64`

Ripple to `jobs_list.go`, `jobs_retry.go`, `jobs_dismiss.go` — update field names.

---

## Dependency Changes (`go.mod`)

**Remove** (after Phase 2):
- `google.golang.org/api/sheets/v4`
- `google.golang.org/api/docs/v1`

**Keep:**
- `google.golang.org/api/drive/v3` (read-only, for drive-import)
- `github.com/clerk/clerk-sdk-go/v2`
- `github.com/sashabaranov/go-openai`

---

## Authorization Pattern

All new CRUD endpoints must verify resource ownership. Pattern:

1. Extract `userID` from Clerk JWT claims
2. For class operations: query class, check `class.UserID == userID`
3. For student operations: join through class to verify `class.UserID == userID`
4. For note/report operations: join through student → class to verify ownership

Add repo methods or a shared helper for ownership checks to avoid N+1 queries.

---

## Resolved Questions

1. **`MetadataIndex` removal** — Dropped entirely. Report generator uses `noteRepo` directly.
2. **UploadJob ID type** — Switching from `string` to `int64` affects queue key format and job endpoints. Frontend dismiss requests will send `uploadIds` (int64) — coordinate with Phase 3+.
3. **Report prompt output format** — Update `report_prompt.go` inline during the `report_generator.go` rewrite to request HTML output.
4. **`classGroup`/`student` types** — Extraction stays name-based (GPT returns names from transcript, not DB IDs). The name→ID lookup happens in `upload_process.go` after extraction via `studentRepo.FindByNameAndClass`. No concern — this is the same as today where extraction is name-based and note creation resolves to Drive folders by name. If a name doesn't match any DB student, log a warning and skip (same behavior as the current confidence threshold skip).
