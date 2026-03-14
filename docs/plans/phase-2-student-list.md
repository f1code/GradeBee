# Phase 2: Student List — Detailed Implementation Plan

## Goal

Read class/student data from the user's Google Sheets spreadsheet and display students grouped by class in the frontend. The app creates the `ClassSetup` spreadsheet in the GradeBee folder during Drive setup, pre-populated with headers and example rows.

---

## Context & Constraints

- **No database**: all data lives in Google Drive/Sheets.
- **Scopes**: `drive.file` grants full read/write access to files the app creates. Since the app creates the ClassSetup spreadsheet, it has full access to read and write it — no additional scopes needed. The `spreadsheets.readonly` scope from the original plan is unnecessary and can be dropped.
- **Auth pattern**: frontend sends Clerk session token → backend calls Clerk Backend API to get Google OAuth access token → uses token with Google APIs.
- **Existing infra**: backend uses manual `switch` routing in `handler.go`, `writeJSON` helper, `authenticateRequest` + `getGoogleOAuthToken` from `auth.go`. Frontend uses raw `fetch()` with `useAuth().getToken()`.

---

## Design: App-Created Spreadsheet

The app creates a `ClassSetup` spreadsheet inside `GradeBee/` during the `POST /setup` flow (Phase 1 enhancement). This means:

1. **No discovery problem** — the app knows the spreadsheet exists because it created it.
2. **No scope issues** — `drive.file` covers files the app creates.
3. **No linking step** — no need for the user to paste a URL or for a `config.json`.
4. **The user fills it in** — after setup, the spreadsheet has headers + example rows. The user opens it in Google Sheets (via a link in the UI), deletes the examples, and enters their real class/student data.

### Spreadsheet Format

Created in `GradeBee/` with the name `ClassSetup`. First sheet tab named `Students`.

| Column | Header | Description |
|--------|--------|-------------|
| A | `class` | Class/group name (e.g. "5A", "Year 10 English") |
| B | `student` | Student full name |

Pre-populated example rows (deleted by user):

| class | student |
|-------|---------|
| 5A | Emma Johnson |
| 5A | Liam Smith |
| 5B | Olivia Brown |
| 5B | Noah Davis |

### Discovery

`GET /students` finds the spreadsheet by searching for a file named `ClassSetup` with MIME type `application/vnd.google-apps.spreadsheet` inside the GradeBee root folder. This is reliable because:
- The app created the file, so `drive.file` scope can see it.
- The name + parent + MIME type query is specific enough.
- If the user renames or deletes it, the app returns a clear error.

---

## Implementation Tasks

### Task 1: Enhance `POST /setup` — Create ClassSetup spreadsheet

**File**: `backend/setup.go` (edit)

After creating the folder structure (`GradeBee/uploads/`, `notes/`, `reports/`), add:

1. Create a Google Sheets service (using the same OAuth token)
2. Create a new spreadsheet via Sheets API:
   - Title: `ClassSetup`
   - One sheet tab named `Students`
   - Header row: `class`, `student`
   - Example data rows (4 rows, 2 classes)
   - Bold the header row, freeze it
3. Move the spreadsheet into the `GradeBee/` folder using Drive API (`srv.Files.Update` to set parent)
4. Make this idempotent: before creating, check if `ClassSetup` already exists in GradeBee folder (same pattern as `findOrCreateFolder` but for spreadsheets). Skip creation if it exists.
5. Return the spreadsheet URL alongside the folder URL in the setup response.

**Updated response type**:

```go
type setupResponse struct {
    FolderID       string `json:"folderId"`
    FolderURL      string `json:"folderUrl"`
    SpreadsheetID  string `json:"spreadsheetId"`
    SpreadsheetURL string `json:"spreadsheetUrl"`
}
```

**Sheets API call** (pseudocode):

```go
spreadsheet := &sheets.Spreadsheet{
    Properties: &sheets.SpreadsheetProperties{Title: "ClassSetup"},
    Sheets: []*sheets.Sheet{{
        Properties: &sheets.SheetProperties{Title: "Students"},
        Data: []*sheets.GridData{{
            RowData: []*sheets.RowData{
                headerRow,    // bold: class, student
                exampleRow1,  // 5A, Emma Johnson
                exampleRow2,  // 5A, Liam Smith
                exampleRow3,  // 5B, Olivia Brown
                exampleRow4,  // 5B, Noah Davis
            },
        }},
    }},
}
created, err := sheetsSrv.Spreadsheets.Create(spreadsheet).Do()
```

Then move into the GradeBee folder:

```go
_, err = driveSrv.Files.Update(created.SpreadsheetId, nil).
    AddParents(rootID).
    RemoveParents("root").
    Do()
```

### Task 2: Backend — `GET /students` endpoint

**File**: `backend/students.go` (new)

**Behavior**:

1. Authenticate request (reuse `authenticateRequest`)
2. Get Google OAuth token (reuse `getGoogleOAuthToken`)
3. Find the ClassSetup spreadsheet:
   - Create a Drive service
   - Find the GradeBee root folder ID
   - Search for `ClassSetup` spreadsheet in that folder:
     ```
     name='ClassSetup' and '<rootID>' in parents
       and mimeType='application/vnd.google-apps.spreadsheet'
       and trashed=false
     ```
   - If not found, return `{"error": "no_spreadsheet", "message": "ClassSetup spreadsheet not found. Try running setup again."}`
4. Read the spreadsheet using Google Sheets API:
   - Range: `Students!A:B` (the `Students` tab, columns A and B)
   - Parse rows: skip row 1 (header), extract `class` and `student`
   - Skip rows where either field is empty
   - Strip leading/trailing whitespace from both fields
5. Group students by class, sort classes alphabetically, sort students alphabetically within each class
6. Return response:

```json
{
  "spreadsheetUrl": "https://docs.google.com/spreadsheets/d/XXXXX/edit",
  "classes": [
    {
      "name": "5A",
      "students": [
        {"name": "Emma Johnson"},
        {"name": "Liam Smith"}
      ]
    },
    {
      "name": "5B",
      "students": [
        {"name": "Olivia Brown"},
        {"name": "Noah Davis"}
      ]
    }
  ]
}
```

**Go types**:

```go
type studentsResponse struct {
    SpreadsheetURL string       `json:"spreadsheetUrl"`
    Classes        []classGroup `json:"classes"`
}

type classGroup struct {
    Name     string    `json:"name"`
    Students []student `json:"students"`
}

type student struct {
    Name string `json:"name"`
}
```

**Error responses**:

| Status | Condition | Body |
|--------|-----------|------|
| 401 | Auth failure | `{"error": "..."}` |
| 502 | Google token retrieval fails | `{"error": "..."}` |
| 404 | ClassSetup spreadsheet not found | `{"error": "no_spreadsheet", "message": "..."}` |
| 422 | Spreadsheet has no data rows (only header or empty) | `{"error": "empty_spreadsheet", "message": "..."}` |
| 500 | Sheets API call fails | `{"error": "..."}` |

### Task 3: Backend — Extract reusable helpers

**File**: `backend/google.go` (new)

The pattern of "authenticate + get Google token + create service" is repeated in `setup.go` and will be repeated in `students.go`. Extract shared helpers:

```go
// googleServices holds authenticated Google API clients.
type googleServices struct {
    Drive  *drive.Service
    Sheets *sheets.Service
    User   *clerkUser
}

// newGoogleServices authenticates the request and returns Drive + Sheets services.
func newGoogleServices(r *http.Request) (*googleServices, error) {
    user, err := authenticateRequest(r)
    if err != nil {
        return nil, &apiError{Status: http.StatusUnauthorized, Err: err}
    }
    accessToken, err := getGoogleOAuthToken(user.UserID)
    if err != nil {
        return nil, &apiError{Status: http.StatusBadGateway, Err: err}
    }
    tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
    driveSrv, err := drive.NewService(r.Context(), option.WithTokenSource(tokenSource))
    if err != nil {
        return nil, &apiError{Status: http.StatusInternalServerError, Err: err}
    }
    sheetsSrv, err := sheets.NewService(r.Context(), option.WithTokenSource(tokenSource))
    if err != nil {
        return nil, &apiError{Status: http.StatusInternalServerError, Err: err}
    }
    return &googleServices{Drive: driveSrv, Sheets: sheetsSrv, User: user}, nil
}

// apiError is an error that carries an HTTP status code.
type apiError struct {
    Status  int
    Err     error
    Code    string // machine-readable error code, e.g. "no_spreadsheet"
    Message string // human-readable message
}

func (e *apiError) Error() string { return e.Err.Error() }

// writeAPIError writes an apiError as a JSON response.
func writeAPIError(w http.ResponseWriter, err *apiError) {
    resp := map[string]string{"error": err.Code}
    if err.Message != "" {
        resp["message"] = err.Message
    }
    if err.Code == "" {
        resp["error"] = err.Err.Error()
    }
    writeJSON(w, err.Status, resp)
}
```

Also move `findOrCreateFolder` here from `setup.go` — it's a general Drive utility.

Add a helper to find the GradeBee root folder:

```go
// getGradeBeeRootID finds the GradeBee root folder.
func getGradeBeeRootID(srv *drive.Service) (string, error) {
    q := "name='GradeBee' and 'root' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false"
    result, err := srv.Files.List().Q(q).Fields("files(id)").Do()
    if err != nil {
        return "", err
    }
    if len(result.Files) == 0 {
        return "", fmt.Errorf("GradeBee folder not found — run setup first")
    }
    return result.Files[0].Id, nil
}
```

### Task 4: Backend — Refactor `setup.go` to use shared helpers

**File**: `backend/setup.go` (edit)

- Replace inline auth + token + Drive service creation with `newGoogleServices(r)`
- Remove `findOrCreateFolder` (now in `google.go`)
- Use `svc.Sheets` for creating the ClassSetup spreadsheet (Task 1)
- `ensureDriveFolders` now takes `*googleServices` instead of raw `accessToken`

### Task 5: Backend — Register `GET /students` route

**File**: `backend/handler.go` (edit)

Add to the `switch`:

```go
case path == "students" && r.Method == http.MethodGet:
    handleGetStudents(w, r)
```

### Task 6: Backend — Unit tests (pure parsing)

**File**: `backend/students_test.go` (new)

Extract the row-parsing and grouping logic into a pure function:

```go
// parseStudentRows takes raw spreadsheet values ([][]interface{}) and returns
// grouped classes. First row is assumed to be a header and is skipped.
func parseStudentRows(rows [][]interface{}) ([]classGroup, error)
```

Test cases:
1. Valid data → correct grouping and alphabetical sorting
2. Empty rows (no data rows after header) → error
3. Rows with missing class or student → skipped
4. Extra columns → ignored (only A and B used)
5. Whitespace in values → trimmed
6. Single class → works
7. Header only → error (empty spreadsheet)
8. Duplicate class names in non-contiguous rows → merged correctly

### Task 6b: Backend — Handler-level tests (HTTP integration with mocked externals)

To test the handlers at the HTTP level, the external dependencies (Clerk auth, Google Drive/Sheets APIs) need to be mockable. The current code calls these directly via package-level functions and `http.DefaultClient`, which makes testing difficult. This task introduces a thin interface layer for testability.

#### Step 1: Make external calls injectable

**File**: `backend/deps.go` (new)

Define an interface for the external dependencies that handlers need:

```go
// deps abstracts external service calls for testability.
type deps interface {
    // Authenticate validates the request and returns the user.
    Authenticate(r *http.Request) (*clerkUser, error)
    // GoogleServices returns authenticated Google API clients for the user.
    GoogleServices(r *http.Request) (*googleServices, error)
}

// prodDeps is the real implementation that calls Clerk + Google APIs.
type prodDeps struct{}

func (prodDeps) Authenticate(r *http.Request) (*clerkUser, error) {
    return authenticateRequest(r)
}

func (prodDeps) GoogleServices(r *http.Request) (*googleServices, error) {
    return newGoogleServices(r)
}
```

The handlers receive deps as a package-level variable (set during init, overridable in tests):

```go
// serviceDeps is the active dependency implementation. Tests override this.
var serviceDeps deps = prodDeps{}
```

Handlers call `serviceDeps.Authenticate(r)` and `serviceDeps.GoogleServices(r)` instead of the raw functions directly.

#### Step 2: Mock Google APIs with `httptest` servers

For handler tests that need to exercise the Sheets/Drive logic (not just auth), use `net/http/httptest` to stand up fake Google API servers. The Google client libraries accept a custom HTTP client and base URL via `option.WithHTTPClient` and `option.WithEndpoint`, so the mock Sheets/Drive servers can be injected through the `googleServices` returned by the mock deps.

**File**: `backend/testhelpers_test.go` (new, test-only)

```go
// mockDeps implements deps for testing.
type mockDeps struct {
    user   *clerkUser
    err    error
    sheets *sheets.Service // backed by httptest server
    drive  *drive.Service  // backed by httptest server
}

func (m *mockDeps) Authenticate(r *http.Request) (*clerkUser, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.user, nil
}

func (m *mockDeps) GoogleServices(r *http.Request) (*googleServices, error) {
    if m.err != nil {
        return nil, m.err
    }
    return &googleServices{Drive: m.drive, Sheets: m.sheets, User: m.user}, nil
}

// newFakeSheetsServer returns an httptest.Server that responds to
// Sheets API calls (spreadsheets.values.get) with configurable data.
func newFakeSheetsServer(values [][]interface{}) *httptest.Server { ... }

// newFakeDriveServer returns an httptest.Server that responds to
// Drive API calls (files.list) with configurable file listings.
func newFakeDriveServer(files ...*drive.File) *httptest.Server { ... }
```

#### Step 3: Handler test cases

**File**: `backend/handler_test.go` (new)

Use `httptest.NewRecorder` + direct calls to `Handle(w, r)` (the Scaleway entrypoint). Override `serviceDeps` in each test, restore it in cleanup.

**`GET /students` tests:**

1. **No auth header → 401**
   - `mockDeps.err = errUnauthorized`
   - Assert: status 401, JSON `{"error": "..."}`

2. **Google token failure → 502**
   - `mockDeps` authenticates OK but `GoogleServices` returns error
   - Assert: status 502

3. **ClassSetup spreadsheet not found → 404**
   - Fake Drive server returns empty file list for the ClassSetup query
   - Assert: status 404, `{"error": "no_spreadsheet", ...}`

4. **Empty spreadsheet (header only) → 422**
   - Fake Sheets server returns only the header row
   - Assert: status 422, `{"error": "empty_spreadsheet", ...}`

5. **Valid spreadsheet → 200 with grouped students**
   - Fake Sheets server returns header + 4 data rows (2 classes)
   - Assert: status 200, response body matches expected `studentsResponse` structure
   - Verify alphabetical sorting of classes and students

6. **Rows with missing fields → skipped gracefully**
   - Fake Sheets server returns rows with empty class/student cells
   - Assert: status 200, only valid rows appear in response

**`POST /setup` tests:**

7. **No auth header → 401**
   - Assert: status 401

8. **Successful setup → 200 with folder + spreadsheet URLs**
   - Fake Drive server handles folder creation queries
   - Fake Sheets server handles spreadsheet creation
   - Assert: status 200, response includes `folderId`, `folderUrl`, `spreadsheetId`, `spreadsheetUrl`

9. **Idempotent setup → 200 (reuses existing folder + spreadsheet)**
   - Fake Drive server returns existing folder/spreadsheet on search queries
   - Assert: status 200, no creation calls made

**`GET /health` tests (sanity):**

10. **Returns 200 OK** — no mocking needed, direct call

**Routing/CORS tests:**

11. **Unknown route → 404**
12. **OPTIONS preflight → 204 with CORS headers**

#### Step 4: Test execution

Add a `test` target to `backend/Makefile`:

```makefile
test:
	go test -v -count=1 ./...
```

Ensure `go test ./...` is also added to the pre-commit hook (or at least documented as a manual step before pushing).

### Task 7: Frontend — Update DriveSetup to show spreadsheet link

**File**: `frontend/src/components/DriveSetup.tsx` (edit)

The setup response now includes `spreadsheetUrl`. After successful setup:
- Show the existing "Open GradeBee folder in Drive" link
- Add a second link: "Open ClassSetup spreadsheet" pointing to `spreadsheetUrl`
- Add instructional text: "Add your students to the ClassSetup spreadsheet, then continue."
- Add a "Continue" button that transitions to the student list view

**Updated interface**:

```typescript
interface SetupResult {
  folderId: string
  folderUrl: string
  spreadsheetId: string
  spreadsheetUrl: string
}
```

### Task 8: Frontend — Student list component

**File**: `frontend/src/components/StudentList.tsx` (new)

**States**: `'loading' | 'empty' | 'error' | 'success'`

**Behavior**:

1. On mount, call `GET ${apiUrl}/students` with auth header
2. Handle responses:
   - `no_spreadsheet` (404) → show error + "Run setup again" prompt
   - `empty_spreadsheet` (422) → show message: "No students found. Add your students to the ClassSetup spreadsheet." with a link to open the spreadsheet
   - Success → render students grouped by class
3. Each class renders as a section with:
   - Class name as heading
   - Student count in parentheses
   - List of student names
4. Show a link to the spreadsheet at the top ("Edit students in Google Sheets")
5. Include a refresh button to re-fetch after user edits the spreadsheet
6. All elements get `data-testid` attributes for e2e testing

**Types**:

```typescript
interface Student {
  name: string
}

interface ClassGroup {
  name: string
  students: Student[]
}

interface StudentsResponse {
  spreadsheetUrl: string
  classes: ClassGroup[]
}
```

### Task 9: Frontend — Update App.tsx for navigation flow

**File**: `frontend/src/App.tsx` (edit)

After Phase 2, the signed-in flow becomes:

```
SignedIn:
  if (!setupDone) → <DriveSetup onComplete={markSetupDone} />
  else → <StudentList />
```

**Implementation**:
- Add `setupDone` state, persisted in `localStorage` as a hint (avoids showing DriveSetup on every reload)
- On mount when `setupDone` is true, go straight to `<StudentList />`
- `DriveSetup` calls `onComplete(result)` prop when setup succeeds — this sets `setupDone = true` in localStorage and state
- If `StudentList` gets an error suggesting setup hasn't been run, reset `setupDone` to show DriveSetup again
- The DriveSetup success screen (with spreadsheet link + "Continue") is now a transition step — "Continue" triggers `onComplete`

### Task 10: Frontend — Styling

**File**: `frontend/src/index.css` (edit)

Add styles for:

```css
/* Student list */
.student-list { margin-top: 1rem; }

.student-list .toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.5rem;
}

.student-list .toolbar a { color: #4f46e5; }

.class-group {
  margin-bottom: 1.5rem;
}

.class-group h3 {
  margin: 0 0 0.5rem;
  font-size: 1.1rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.class-group .count {
  font-size: 0.8rem;
  color: #6b7280;
  font-weight: normal;
}

.class-group ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.class-group li {
  padding: 0.4rem 0;
  border-bottom: 1px solid #f3f4f6;
}

/* Empty / info states */
.info-box {
  background: #f9fafb;
  border: 1px solid #e5e7eb;
  border-radius: 0.5rem;
  padding: 1.5rem;
  text-align: center;
}

.info-box a { color: #4f46e5; }
```

### Task 11: E2E tests

**File**: `e2e/students.spec.ts` (new)

Test cases (with mocked API responses via `page.route()`):

1. **Student list loads and displays grouped by class** — mock `GET /students` → success response, verify class headings, student names, counts
2. **Empty spreadsheet** — mock `GET /students` → 422 `empty_spreadsheet`, verify info message and spreadsheet link shown
3. **Spreadsheet not found** — mock `GET /students` → 404 `no_spreadsheet`, verify error message
4. **Refresh re-fetches data** — mock returns empty first, then data on second call; click refresh; verify data appears
5. **Setup flow transitions to student list** — mock `POST /setup` → success, click Continue, verify student list loads

### Task 12: Update E2E setup test

**File**: `e2e/drive-setup.spec.ts` (edit)

Update the mock setup response to include the new `spreadsheetId` and `spreadsheetUrl` fields. Verify the spreadsheet link and "Continue" button appear.

### Task 13: Vendor, build, verify

- Run `go mod vendor` in `backend/` after adding Sheets API import
- Run `go build .` to verify compilation
- Run `make lint` in `backend/` to verify linting passes
- Run frontend build (`npm run build` in `frontend/`)
- Run e2e tests

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `backend/google.go` | **New** | Shared `newGoogleServices`, `apiError`, `findOrCreateFolder`, `getGradeBeeRootID` |
| `backend/deps.go` | **New** | `deps` interface + `prodDeps` implementation for dependency injection |
| `backend/students.go` | **New** | `GET /students` handler + row parsing/grouping |
| `backend/students_test.go` | **New** | Unit tests for `parseStudentRows` pure function |
| `backend/testhelpers_test.go` | **New** | `mockDeps`, fake Sheets/Drive httptest servers |
| `backend/handler_test.go` | **New** | Handler-level HTTP tests (auth, routing, CORS, endpoint behavior) |
| `backend/setup.go` | **Edit** | Create ClassSetup spreadsheet; refactor to use shared helpers + `serviceDeps` |
| `backend/handler.go` | **Edit** | Add `students` route |
| `backend/auth.go` | No change | |
| `backend/Makefile` | **Edit** | Add `test` target |
| `frontend/src/components/StudentList.tsx` | **New** | Student list display component |
| `frontend/src/components/DriveSetup.tsx` | **Edit** | Show spreadsheet link + Continue button after setup |
| `frontend/src/App.tsx` | **Edit** | Add `setupDone` state, sequential flow |
| `frontend/src/index.css` | **Edit** | Add student list + info box styles |
| `e2e/students.spec.ts` | **New** | E2E tests for student list |
| `e2e/drive-setup.spec.ts` | **Edit** | Update mock response to include spreadsheet fields |

---

## Suggested Implementation Order

1. **Task 3**: Extract reusable helpers (`google.go`)
2. **Task 6b step 1**: Create `deps.go` with dependency injection interface
3. **Task 4**: Refactor `setup.go` to use shared helpers + `serviceDeps`
4. **Task 1**: Enhance `POST /setup` to create ClassSetup spreadsheet
5. **Task 2**: `GET /students` endpoint
6. **Task 5**: Register route in `handler.go`
7. **Task 6b step 2**: Create test helpers (`testhelpers_test.go`)
8. **Task 6**: Unit tests for `parseStudentRows`
9. **Task 6b step 3**: Handler-level tests (`handler_test.go`)
10. **Task 7**: Update DriveSetup component (spreadsheet link + Continue)
11. **Task 9**: Update App.tsx for navigation flow
12. **Task 8**: StudentList component
13. **Task 10**: Styling
14. **Task 12**: Update existing e2e tests
15. **Task 11**: New e2e tests
16. **Task 13**: Vendor, build, verify

---

## Scope Note

This plan **removes** the `POST /students/link` endpoint and `config.json` approach from the earlier draft. Since the app creates the spreadsheet, there is no linking step. The user's only action is to edit the spreadsheet content in Google Sheets.

The `spreadsheets.readonly` scope can potentially be removed from the Clerk Google OAuth config, since `drive.file` covers reading spreadsheets the app created. However, keep it for now in case a future phase needs to read user-owned spreadsheets not created by the app.
