# Test `drive_import_example.go`

## Goal
Add unit tests for `handleDriveImportExample` covering input validation, auth, MIME filtering, size limits, extractor paths, and storage.

## Approach
Now that `GetDriveClient` returns the `DriveClient` interface and `stubDriveClient` exists in `testutil_test.go`, all tests use simple stubs via `mockDepsAll` — same pattern as `drive_import_test.go`.

## Plan

### 1. Add `stubExampleExtractor` to `testutil_test.go`
- Implements `ExampleExtractor` interface: `ExtractText(ctx, filename, data) (string, error)`
- Fields: `result string`, `err error`, `gotFilename string`, `gotData []byte`

### 2. Fix `mockDepsAll.GetExampleExtractor()`
- Currently hardcoded to `return nil, fmt.Errorf("not configured")`
- Add `exampleExtractor ExampleExtractor` and `exampleExtractorErr error` fields
- Return those (matching the pattern used by `GetExtractor`, `GetReportGenerator`, etc.)

### 3. Add `stubExampleStore` to `testutil_test.go`
- Implements `ExampleStore`: `ListExamples`, `UploadExample`, `DeleteExample`
- `UploadExample` records calls (name, content) and returns a canned `*ReportExample`
- `uploadErr` field for simulating store failures

### 4. Create `drive_import_example_test.go` with these tests:

Add `newDriveImportExampleReq(t, userID, fileID, fileName)` helper (mirrors `newDriveImportReq` from `drive_import_test.go`).

**Input validation (no mocks needed — rejected before auth):**
- `TestDriveImportExample_InvalidJSON` → 400
- `TestDriveImportExample_MissingFileID` → 400
- `TestDriveImportExample_MissingFileName` → 400
- `TestDriveImportExample_BlankFileName` → 400 (whitespace-only)

**Auth:**
- `TestDriveImportExample_NoSession` → 403

**Drive client errors:**
- `TestDriveImportExample_DriveClientError` → 502 (`GetDriveClient` returns error)
- `TestDriveImportExample_FileMetaError` → 404 (`GetFileMeta` returns error)
- `TestDriveImportExample_DownloadError` → 500 (`DownloadFile` returns error)

**MIME type filtering:**
- `TestDriveImportExample_DisallowedMIME` → 400 (e.g. `application/zip`)

**Size limit:**
- `TestDriveImportExample_ExceedsSizeLimit` → 400 (provide exactly `maxReportImportBytes` of data)

**Extractor paths (PDF/image files):**
- `TestDriveImportExample_PDFExtractsText` → 200, verify `stubExampleExtractor` called, content stored
- `TestDriveImportExample_ImageExtractsText` → 200, same with `image/png`
- `TestDriveImportExample_ExtractorUnavailable` → 500 (`GetExampleExtractor` returns error)
- `TestDriveImportExample_ExtractorFails` → 500 (`ExtractText` returns error)
- `TestDriveImportExample_ExtractorReturnsEmpty` → 400 ("no text content")

**Text file path (bypasses extractor):**
- `TestDriveImportExample_PlainTextDirect` → 200, content stored directly, extractor NOT called
- `TestDriveImportExample_MarkdownDirect` → 200, same for `text/markdown`
- `TestDriveImportExample_EmptyTextFile` → 400 ("no text content")

**Storage:**
- `TestDriveImportExample_StoreFailure` → 500 (`UploadExample` returns error)

**Happy path:**
- `TestDriveImportExample_Success` → 200, response body is the stored `ReportExample` JSON with correct name/content

## Files to Modify
- `backend/testutil_test.go` — add `stubExampleExtractor`, `stubExampleStore`, fix `mockDepsAll.GetExampleExtractor()`

## New Files
- `backend/drive_import_example_test.go` — all tests + `newDriveImportExampleReq` helper

## Risks
- `mockDepsAll.GetExampleExtractor()` change is safe — currently returns error unconditionally, no existing test relies on it succeeding.
