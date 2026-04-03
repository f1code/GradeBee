# Tygo Cleanup: Filter Generated Types & Wire CI

**Goal:** Fix review issues from tygo implementation — filter generated types to API-relevant ones only, wire check into pre-commit and root Makefile, add gitattributes, remove unused `ReportDetail`.

---

## Task 1: Add `include_types` to tygo.yaml

**File:** `backend/tygo.yaml`

Replace the current config with an explicit `include_types` list covering only frontend-consumed types. Derived from what `api.ts` actually imports plus the response wrapper types:

```yaml
packages:
  - path: "github.com/nicogaller/gradebee/backend"
    output_path: "../frontend/src/api-types.gen.ts"
    type_mappings:
      "time.Time": "string"
    include_types:
      # Core entities
      - Note
      - Student
      - ClassWithCount
      - Report
      - ReportExample
      - ReportSummary
      # List response wrappers
      - ListNotesResponse
      - ListClassesResponse
      - ListStudentsResponse
      - ListReportsResponse
      - ListExamplesResponse
      # Request/response types
      - ReportResult
      - ReportDetail
      - GenerateReportsHTTPRequest
      - GenerateReportsHTTPResponse
      - ReportStudentInput
      - RegenerateReportHTTPRequest
      - UploadResponse
      - DriveImportRequest
      - DriveImportResponse
      - GoogleTokenResponse
      - UploadJob
      - NoteLink
      - JobListResponse
```

Run `cd backend && make generate` and verify the file shrinks to ~100-150 lines with no `any` type exports.

Note: `ClassWithCount extends Class` — so `Class` must also be included if tygo needs it for the `extends`. Check output; if `Class` is missing, add it.

## Task 2: Add `.gitattributes` for generated file

**File:** `.gitattributes` (create)

```
frontend/src/api-types.gen.ts linguist-generated=true
```

## Task 3: Wire `check-types` into pre-commit hook

**File:** `.husky/pre-commit`

Add a check: if any `.go` files are staged, run `make -C backend check-types`. Add it inside the existing Go-files-staged block:

```bash
if git diff --cached --name-only | grep -q '\.go$'; then
  cd backend && make lint || exit 1
  cd backend && make check-types || exit 1
fi
```

Note: `check-types` runs `tygo generate` then checks git diff — this will auto-detect if the generated file needs updating.

## Task 4: Import and re-export `ReportDetail` if used, or remove from plan

Check if `ReportDetail` is used anywhere in the frontend. If not, skip importing it. If it should be used (e.g. for `getReport` return type), add to `api.ts` imports.

Run: `grep -r 'ReportDetail' frontend/src/`

## Task 5: Regenerate, verify, commit

```bash
cd backend && make generate && make check-types
cd frontend && npx tsc --noEmit && npx vitest run
```

Single commit: `fix: filter tygo output to API types, wire check-types into pre-commit`
