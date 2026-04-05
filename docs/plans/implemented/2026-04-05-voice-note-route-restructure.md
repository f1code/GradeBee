# Voice Note Route Restructure

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Rename files and routes to group all voice-note endpoints under `/voice-notes/*`, distinguishing them from report-example uploads. Rename backend Go files to use a `voice_note_` prefix. Consolidate three small jobs handler files into one.

**Architecture:** Rename routes (`/upload` → `/voice-notes/upload`, `/drive-import` → `/voice-notes/drive-import`, `/jobs` → `/voice-notes/jobs`), rename and consolidate handler files, and update frontend API calls. Pure rename/move — no logic changes.

**Tech Stack:** Go backend (handler routing), TypeScript frontend (fetch URLs)

---

### Task 1: Rename and consolidate backend handler files

**Files:**
- Rename: `backend/upload.go` → `backend/voice_note_upload.go`
- Rename: `backend/upload_test.go` → `backend/voice_note_upload_test.go`
- Rename: `backend/drive_import.go` → `backend/voice_note_drive_import.go`
- Rename: `backend/drive_import_test.go` → `backend/voice_note_drive_import_test.go`
- Consolidate: `backend/jobs_list.go` + `backend/jobs_retry.go` + `backend/jobs_dismiss.go` → `backend/voice_note_jobs.go`
- Consolidate: `backend/jobs_list_test.go` + `backend/jobs_retry_test.go` → `backend/voice_note_jobs_test.go`

**Step 1: Rename upload and drive import files**

```bash
cd backend && \
  git mv upload.go voice_note_upload.go && \
  git mv upload_test.go voice_note_upload_test.go && \
  git mv drive_import.go voice_note_drive_import.go && \
  git mv drive_import_test.go voice_note_drive_import_test.go
```

**Step 2: Create consolidated `voice_note_jobs.go`**

Create `backend/voice_note_jobs.go` by concatenating the three jobs handler files into one. The file should have:

- One `package handler` declaration
- Combined imports (deduplicated)
- All types: `JobListResponse`, `jobRetryResponse`, `dismissRequest`
- All handlers: `handleJobList`, `handleJobRetry`, `handleJobDismiss`

Keep the functions in this order: list, retry, dismiss (read-first, then mutations).

```go
// voice_note_jobs.go handles the voice note job endpoints:
//   GET  /voice-notes/jobs         — list jobs grouped by status
//   POST /voice-notes/jobs/retry   — retry failed jobs
//   POST /voice-notes/jobs/dismiss — dismiss completed/failed jobs
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/clerk/clerk-sdk-go/v2"
)

// JobListResponse groups jobs by their processing state.
type JobListResponse struct {
	Active []VoiceNoteJob `json:"active"`
	Failed []VoiceNoteJob `json:"failed"`
	Done   []VoiceNoteJob `json:"done"`
}

func handleJobList(w http.ResponseWriter, r *http.Request) {
	// ... (copy entire function body from jobs_list.go unchanged)
}

type jobRetryResponse struct {
	RetriedCount int `json:"retriedCount"`
}

func handleJobRetry(w http.ResponseWriter, r *http.Request) {
	// ... (copy entire function body from jobs_retry.go unchanged)
}

type dismissRequest struct {
	UploadIDs []int64 `json:"uploadIds"`
}

func handleJobDismiss(w http.ResponseWriter, r *http.Request) {
	// ... (copy entire function body from jobs_dismiss.go unchanged)
}
```

Then delete the old files:

```bash
cd backend && git rm jobs_list.go jobs_retry.go jobs_dismiss.go
```

**Step 3: Create consolidated `voice_note_jobs_test.go`**

Create `backend/voice_note_jobs_test.go` by concatenating the two test files. The `clerkCtx` helper is defined in `jobs_list_test.go` and used by both — it stays.

- One `package handler` declaration
- Combined imports (deduplicated)
- `clerkCtx` helper (from `jobs_list_test.go`)
- All test functions from both files

```go
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

func clerkCtx(r *http.Request, userID string) *http.Request {
	// ... (copy from jobs_list_test.go)
}

// --- List tests ---

func TestJobList_GroupsByStatus(t *testing.T) {
	// ... (copy from jobs_list_test.go unchanged)
}

func TestJobList_EmptyUser(t *testing.T) {
	// ... (copy from jobs_list_test.go unchanged)
}

func TestJobList_SortedDescending(t *testing.T) {
	// ... (copy from jobs_list_test.go unchanged)
}

// --- Retry tests ---

func TestJobRetry_RetriesFailedOnly(t *testing.T) {
	// ... (copy from jobs_retry_test.go unchanged)
}

func TestJobRetry_NoFailedJobs(t *testing.T) {
	// ... (copy from jobs_retry_test.go unchanged)
}
```

Then delete the old test files:

```bash
cd backend && git rm jobs_list_test.go jobs_retry_test.go
```

**Step 4: Update file header comments**

In each renamed/new file, update the first-line comment:

- `voice_note_upload.go`: `// voice_note_upload.go handles POST /voice-notes/upload — receives an audio file via multipart/form-data and saves it to local disk + the voice_notes table.`
- `voice_note_drive_import.go`: `// voice_note_drive_import.go handles POST /voice-notes/drive-import — downloads a Google Drive file to local disk, creates a voice_notes row, and dispatches an async processing job.`

**Step 5: Run tests**

```bash
cd backend && go test -count=1 ./...
```

Expected: PASS (no logic changes).

**Step 6: Lint + commit**

```bash
cd backend && make lint
git add -A backend/
git commit -m "refactor: rename and consolidate voice note handler files"
```

---

### Task 2: Update backend routes

**Files:**
- Modify: `backend/handler.go`

**Step 1: Update the route entries in the switch statement**

Find and replace these five route cases:

```go
// before:
case path == "upload" && r.Method == http.MethodPost:
// after:
case path == "voice-notes/upload" && r.Method == http.MethodPost:

// before:
case path == "drive-import" && r.Method == http.MethodPost:
// after:
case path == "voice-notes/drive-import" && r.Method == http.MethodPost:

// before:
case path == "jobs" && r.Method == http.MethodGet:
// after:
case path == "voice-notes/jobs" && r.Method == http.MethodGet:

// before:
case path == "jobs/retry" && r.Method == http.MethodPost:
// after:
case path == "voice-notes/jobs/retry" && r.Method == http.MethodPost:

// before:
case path == "jobs/dismiss" && r.Method == http.MethodPost:
// after:
case path == "voice-notes/jobs/dismiss" && r.Method == http.MethodPost:
```

Leave `drive-import-example` and `google-token` unchanged — they're separate domains.

**Step 2: Run tests + lint**

```bash
cd backend && go test -count=1 ./... && make lint
```

Expected: PASS. Handler tests call functions directly, not through the router.

**Step 3: Commit**

```bash
git add backend/handler.go
git commit -m "refactor: move voice note routes under /voice-notes/* prefix"
```

---

### Task 3: Update frontend API URLs

**Files:**
- Modify: `frontend/src/api.ts`

**Step 1: Update all voice-note-related fetch URLs**

Find and replace these five URL strings in `frontend/src/api.ts`:

```typescript
// uploadAudio function (~line 259):
// before:
`${apiUrl}/upload`
// after:
`${apiUrl}/voice-notes/upload`

// importFromDrive function (~line 475):
// before:
`${apiUrl}/drive-import`
// after:
`${apiUrl}/voice-notes/drive-import`

// fetchJobs function (~line 494):
// before:
`${apiUrl}/jobs`
// after:
`${apiUrl}/voice-notes/jobs`

// retryFailedJobs function (~line 506):
// before:
`${apiUrl}/jobs/retry`
// after:
`${apiUrl}/voice-notes/jobs/retry`

// dismissJobs function (~line 521):
// before:
`${apiUrl}/jobs/dismiss`
// after:
`${apiUrl}/voice-notes/jobs/dismiss`
```

Do NOT change `/drive-import-example` — that's a report-example endpoint, not voice notes.

**Step 2: Verify frontend compiles**

```bash
cd frontend && npm run build
```

Expected: success (no type changes, just URL strings).

**Step 3: Commit**

```bash
git add frontend/src/api.ts
git commit -m "refactor: update frontend API URLs for /voice-notes/* routes"
```

---

### Task 4: Update ARCHITECTURE.md

**Files:**
- Modify: `backend/ARCHITECTURE.md`

**Step 1: Update the route table**

Replace these rows in the routing table:

| Old Path | New Path | Description |
|----------|----------|-------------|
| `POST /upload` | `POST /voice-notes/upload` | Upload audio to disk + dispatch job |
| `POST /drive-import` | `POST /voice-notes/drive-import` | Download from Drive + dispatch job |
| `GET /jobs` | `GET /voice-notes/jobs` | List user's async voice note jobs |
| `POST /jobs/retry` | `POST /voice-notes/jobs/retry` | Retry failed jobs |
| `POST /jobs/dismiss` | `POST /voice-notes/jobs/dismiss` | Dismiss completed/failed jobs |

**Step 2: Update the file-by-file reference table**

Replace these rows:

| Old | New |
|-----|-----|
| `upload.go` → `POST /upload — multipart audio → disk + uploads table + dispatch job` | `voice_note_upload.go` → `POST /voice-notes/upload — multipart audio → disk + voice_notes table + dispatch job` |
| `drive_import.go` → `POST /drive-import — download from Drive → disk + uploads table + dispatch job` | `voice_note_drive_import.go` → `POST /voice-notes/drive-import — download from Drive → disk + voice_notes table + dispatch job` |
| `jobs_list.go` → `GET /jobs — list user's async upload jobs grouped by status` | *(delete row)* |
| `jobs_retry.go` → `POST /jobs/retry — reset failed jobs to queued and republish` | *(delete row)* |
| `jobs_dismiss.go` → `POST /jobs/dismiss — remove completed/failed jobs, mark uploads processed` | *(delete row)* |

Add one new row:

| `voice_note_jobs.go` | `GET /voice-notes/jobs`, `POST /voice-notes/jobs/retry`, `POST /voice-notes/jobs/dismiss` — voice note job list, retry, dismiss handlers |

**Step 3: Commit**

```bash
git add backend/ARCHITECTURE.md
git commit -m "docs: update architecture for /voice-notes/* routes and renamed files"
```

---

## Decisions

- **`/voice-notes/*` not `/voice-note/*`** — plural is conventional for REST resource collections.
- **Consolidate 3 jobs files → 1** — they're small (215 lines total), tightly coupled, and the 3-file split was noise.
- **`drive-import-example` stays unchanged** — belongs to report-example domain, not voice notes.
- **`google-token` stays unchanged** — utility endpoint, not voice-note-specific.
- **Handler function names unchanged** — `handleUpload`, `handleDriveImport`, `handleJobList` etc. are internal; the file prefix provides domain grouping.
- **No logic changes** — purely rename/reorganization.
