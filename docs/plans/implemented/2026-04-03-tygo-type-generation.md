# Tygo Type Generation: Go → TypeScript

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Auto-generate frontend TypeScript types from backend Go structs using tygo, so interface mismatches are caught at build time instead of at runtime.

**Architecture:** Add tygo to the backend toolchain. Replace `map[string]any` response wrappers with typed structs so tygo can see them. Generate a `api-types.gen.ts` file in the frontend. Replace hand-written interfaces in `api.ts` with imports from the generated file. Add a CI/Makefile check that the generated file is up-to-date.

**Tech Stack:** [tygo](https://github.com/gzuidhof/tygo) (Go→TS codegen), Go struct JSON tags, TypeScript

**Prerequisite:** Complete the `2026-04-03-fix-api-interface-mismatches.md` plan first — the backend structs must be correct before we generate types from them.

---

## Overview

### What tygo does
Reads Go structs with `json` tags and emits TypeScript interfaces. For example:
```go
type Note struct {
    ID        int64   `json:"id"`
    StudentID int64   `json:"studentId"`
    Summary   string  `json:"summary"`
    Transcript *string `json:"transcript"`
}
```
Becomes:
```ts
export interface Note {
    id: number;
    studentId: number;
    summary: string;
    transcript: string | null;
}
```

### What needs to change first
Several handlers use `map[string]any` wrappers (e.g. `map[string]any{"notes": notes}`). These are invisible to tygo. We need typed response structs for:
- `handleListNotes` → `listNotesResponse`
- `handleListClasses` → `listClassesResponse`
- `handleListStudents` → `listStudentsResponse`
- `handleListReports` → `listReportsResponse`
- `handleListReportExamples` → `listExamplesResponse`

---

## Task 1: Add `ReportSummary` struct and replace `map[string]any` wrappers with typed response structs

**Files:**
- Modify: `backend/repo_report.go` (add `ReportSummary`, change `List` return type)
- Modify: `backend/notes.go`
- Modify: `backend/students.go`
- Modify: `backend/reports_handler.go`
- Modify: `backend/report_examples_handler.go`

**Step 1: Add `ReportSummary` struct and update `ReportRepo.List`**

In `backend/repo_report.go`, add a `ReportSummary` struct (no `StudentID` — the list is already scoped by student, and the frontend doesn't use it):
```go
type ReportSummary struct {
	ID        int64  `json:"id"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	CreatedAt string `json:"createdAt"`
}
```

Change `ReportRepo.List` to return `[]ReportSummary` instead of `[]Report`. Update the row scan to match (drop `instructions` from the SELECT or ignore it).

Also in `backend/report_examples.go`, remove `omitempty` from `ReportExample.Content` so tygo generates `content: string` (required), matching the frontend:
```go
// Before
Content string `json:"content,omitempty"`
// After
Content string `json:"content"`
```

**Step 2: Add typed response structs**

In `backend/notes.go`, before `handleListNotes`:
```go
type listNotesResponse struct {
	Notes []Note `json:"notes"`
}
```

Replace the `writeJSON` call in `handleListNotes`:
```go
// Before:
writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
// After:
writeJSON(w, http.StatusOK, listNotesResponse{Notes: notes})
```

In `backend/students.go`, add:
```go
type listClassesResponse struct {
	Classes []ClassWithCount `json:"classes"`
}

type listStudentsItemResponse struct {
	Students []Student `json:"students"`
}
```

Replace in `handleListClasses`:
```go
writeJSON(w, http.StatusOK, listClassesResponse{Classes: classes})
```

Replace in `handleListStudents`:
```go
writeJSON(w, http.StatusOK, listStudentsItemResponse{Students: students})
```

In `backend/reports_handler.go`, add:
```go
type listReportsResponse struct {
	Reports []ReportSummary `json:"reports"`
}
```

Replace in `handleListReports`:
```go
writeJSON(w, http.StatusOK, listReportsResponse{Reports: reports})
```

In `backend/report_examples_handler.go`, add:
```go
type listExamplesResponse struct {
	Examples []ReportExample `json:"examples"`
}
```

Replace in `handleListReportExamples`:
```go
writeJSON(w, http.StatusOK, listExamplesResponse{Examples: examples})
```

**Step 3: Run all tests + lint**

Run: `cd backend && make test && make lint`
Expected: All pass — this is a pure refactor, no behavior change.

**Step 4: Commit**

```bash
git add backend/
git commit -m "refactor: add ReportSummary struct, replace map[string]any response wrappers with typed structs"
```

---

## Task 2: Install and configure tygo

**Files:**
- Modify: `backend/Makefile` (add `generate` target)
- Create: `backend/tygo.yaml`

**Step 1: Install tygo**

Run: `go install github.com/gzuidhof/tygo@latest`

Verify: `tygo --version`

**Step 2: Create tygo config**

Create `backend/tygo.yaml`:
```yaml
packages:
  - path: "github.com/nicogaller/gradebee/backend"
    output_path: "../frontend/src/api-types.gen.ts"
    # Only export types with json tags that the frontend needs
    include_types:
      # Core entities
      - Note
      - Student
      - ClassWithCount
      - Report
      - ReportExample
      - ReportSummary
      # List response wrappers
      - listNotesResponse
      - listClassesResponse
      - listStudentsItemResponse
      - listReportsResponse
      - listExamplesResponse
      # Request/response types
      - reportResult
      - reportDetail
      - generateReportsRequest
      - generateReportsResponse
      - reportStudentInput
      - regenerateReportRequest
      - uploadResponse
      - driveImportRequest
      - driveImportResponse
      - googleTokenResponse
      - UploadJob
      - NoteLink
      - jobListResponse
      # Report examples
      - ReportExampleItem
    # Frontends don't need these
    exclude_types:
      - Class
      - DBReportExample
    type_mappings:
      "time.Time": "string"
```

Note: After creating this file, run tygo and check the output. The `include_types` list may need adjustment based on which types are exported (capitalized) vs unexported. tygo only sees exported types by default. We may need to capitalize some response struct names.

**Step 3: Check which response types need to be exported**

The following types are currently unexported (lowercase): `listNotesResponse`, `listClassesResponse`, `listStudentsItemResponse`, `listReportsResponse`, `listExamplesResponse`, `reportResult`, `reportDetail`, `generateReportsRequest`, `generateReportsResponse`, `reportStudentInput`, `regenerateReportRequest`, `uploadResponse`, `driveImportRequest`, `driveImportResponse`, `googleTokenResponse`, `jobListResponse`.

Rename them all to be exported (capitalized). This is safe because they're only used within the `handler` package.

For example in `backend/reports_handler.go`:
- `reportResult` → `ReportResult`
- `reportDetail` → `ReportDetail`
- `generateReportsRequest` → `GenerateReportsRequest`
- `generateReportsResponse` → `GenerateReportsResponse` (note: name collision with the existing `GenerateReportResponse` in `report_generator.go` — rename that to `GenerateReportResult` or keep distinct)
- `reportStudentInput` → `ReportStudentInput`
- `regenerateReportRequest` → `RegenerateReportHTTPRequest` (to avoid collision with `RegenerateReportRequest` in `report_generator.go`)

And so on for other files. Be careful with name collisions — check each rename.

**Step 4: Run tygo and verify output**

Run: `cd backend && tygo generate`
Check: `cat ../frontend/src/api-types.gen.ts`

Verify it contains the expected interfaces.

**Step 5: Add Makefile target**

Add to `backend/Makefile`:
```makefile
generate:
	tygo generate

check-types:
	tygo generate
	git diff --exit-code ../frontend/src/api-types.gen.ts || (echo "Generated types are out of date. Run 'make generate' in backend/" && exit 1)
```

**Step 6: Run it**

Run: `cd backend && make generate`
Expected: `frontend/src/api-types.gen.ts` is created/updated.

**Step 7: Commit**

```bash
git add backend/tygo.yaml backend/Makefile backend/*.go frontend/src/api-types.gen.ts
git commit -m "feat: add tygo config for Go→TypeScript type generation"
```

---

## Task 3: Replace hand-written frontend interfaces with generated types

**Files:**
- Modify: `frontend/src/api.ts`

**Step 1: Replace interfaces with imports**

At the top of `frontend/src/api.ts`, add:
```ts
import type {
  ClassWithCount as ClassItem,
  Student as StudentItem,
  Note,
  ReportExample as ReportExampleItem,
  ReportResult,
  ReportDetail,
  GenerateReportsResponse,
  ListReportsResponse,
  ListNotesResponse,
  ListClassesResponse,
  ListStudentsItemResponse,
  ListExamplesResponse,
  UploadJob,
  JobListResponse,
  UploadResponse,
  DriveImportResponse,
  GoogleTokenResponse,
} from './api-types.gen'
```

Then delete the hand-written interfaces: `ClassItem`, `StudentItem`, `Note`, `ReportExampleItem`, `ReportResult`, `ReportSummary`, `GenerateReportsResponse`, `UploadJob`, `JobListResponse`.

Re-export them for components that import from `api.ts`:
```ts
export type { ClassItem, StudentItem, Note, ReportExampleItem, ReportResult, ReportDetail, GenerateReportsResponse, UploadJob, JobListResponse }
```

Note: The exact mapping between generated type names and the names used throughout the frontend will require care. Some generated names may differ. Check the generated file and use `as` aliases where needed.

**Step 2: Run type check**

Run: `cd frontend && npx tsc --noEmit`

Fix any type errors — these are real mismatches that the generated types catch!

**Step 3: Run frontend tests**

Run: `cd frontend && npx vitest run`

**Step 4: Commit**

```bash
git add frontend/src/api.ts frontend/src/api-types.gen.ts
git commit -m "feat: use tygo-generated types in frontend api.ts"
```

---

## Task 4: Add generated type check to CI

**Files:**
- Modify: whatever CI config exists (Makefile, GitHub Actions, etc.)

**Step 1: Check current CI setup**

Run: `ls .github/workflows/ 2>/dev/null; cat Makefile | head -30`

**Step 2: Add check**

Add `make -C backend check-types` to the CI pipeline, or to the root `Makefile` if there's a top-level lint/check target. This ensures that if someone changes a Go struct and forgets to regenerate, CI fails.

**Step 3: Test it locally**

Run: `cd backend && make check-types`
Expected: Clean exit (types are up-to-date).

Now test the failure case:
```bash
# Temporarily add a field to a struct
echo '// test' >> ../frontend/src/api-types.gen.ts
cd backend && make check-types  # should fail
git checkout ../frontend/src/api-types.gen.ts  # revert
```

**Step 4: Commit**

```bash
git add .
git commit -m "ci: add check that generated TypeScript types are up-to-date"
```

---

## Task 5: Final verification

**Step 1: Run full backend suite**

Run: `cd backend && make test && make lint`

**Step 2: Run type generation**

Run: `cd backend && make generate && make check-types`

**Step 3: Run full frontend suite**

Run: `cd frontend && npx tsc --noEmit && npx vitest run`

**Step 4: Verify the generated file looks reasonable**

Run: `wc -l frontend/src/api-types.gen.ts` — should be roughly 80-150 lines depending on how many types are included.

Run: `head -5 frontend/src/api-types.gen.ts` — should have a generated-file header comment.

---

## Open Questions

1. ~~**`ReportSummary` vs `Report`**~~: **Resolved** — adding a separate `ReportSummary` Go struct (ID, StartDate, EndDate, CreatedAt). `ReportRepo.List` returns `[]ReportSummary`. This makes the API contract explicit and avoids relying on `omitempty` to define shape.

2. ~~**`time.Time` mapping**~~: **Resolved** — the `type_mappings: "time.Time": "string"` in `tygo.yaml` handles this. `CreatedAt time.Time` → `createdAt: string`, `FailedAt *time.Time` → `failedAt: string | null`. Verify during Task 2 Step 4.

3. ~~**Unexported types**~~: **Resolved** — handled by Task 2 Step 3 which renames all unexported handler types to exported.

4. ~~**`omitempty` handling**~~: **Resolved** — most `omitempty` fields already match frontend expectations (`UploadJob.NoteLinks`, `UploadJob.Error` are optional in both). The one fix needed: remove `omitempty` from `ReportExample.Content` so tygo generates `content: string` (required) matching the frontend. Done in Task 1.
