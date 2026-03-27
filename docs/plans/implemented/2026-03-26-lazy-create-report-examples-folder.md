# Lazy-create report-examples folder

## Goal
Remove the hard dependency on setup having created the `report-examples` subfolder. Instead, create it on demand the first time it's needed. This fixes the case where a user with partial setup sees "report-examples folder not configured, run setup first" with no way to resolve it.

## Proposed changes

### 1. Extract a helper: `ensureReportExamplesFolder`
**File:** `backend/report_examples_handler.go`

Add a helper function:
```go
func ensureReportExamplesFolder(ctx context.Context, svc *googleServices, meta *gradeBeeMetadata) (string, error)
```
- If `meta.ReportExamplesID != ""`, return it immediately.
- Otherwise, create the folder under `meta.FolderID` via `createFolder(svc.Drive, meta.FolderID, "report-examples")`.
- Persist the updated metadata via `setGradeBeeMetadata`.
- Return the new folder ID.
- Error if `meta == nil` or `meta.FolderID == ""` (truly no setup at all).

### 2. Update `handleUploadReportExample`
**File:** `backend/report_examples_handler.go` (line ~62)

Replace the `meta == nil || meta.ReportExamplesID == ""` error block with a call to `ensureReportExamplesFolder`. Use the returned folder ID for the upload.

### 3. Update `handleListReportExamples`
**File:** `backend/report_examples_handler.go` (line ~29)

Keep returning empty list when `ReportExamplesID == ""` — no change needed (listing an empty/nonexistent folder should just return `[]`).

### 4. Update `handleDeleteReportExample`
No change needed — delete operates on a file ID directly.

### 5. Remove `report-examples` from setup's initial folder creation
**File:** `backend/setup.go`

- Remove `{"report-examples", &meta.ReportExamplesID}` from both subfolder lists (lines ~148, ~201).
- Remove `meta.ReportExamplesID == ""` from the `needsUpdate` check (line ~189).

### 6. Update report generation consumers
**File:** `backend/reports_handler.go` (lines ~86, ~169)

These pass `meta.ReportExamplesID` as `ExamplesFolderID`. They should tolerate an empty string (meaning no examples) — verify this is already the case, no change expected.

## Open questions
- Should we also lazy-create other subfolders (uploads, notes, reports) for consistency? (Suggest: not now, keep scope small.)
