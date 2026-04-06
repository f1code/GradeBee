# PDF Extraction Review Fixes — Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Fix issues identified in the code review of the PDF extraction / async extraction feature.

**Architecture:** Small targeted fixes across backend and frontend. No new patterns — tightening existing code.

**Tech Stack:** Go, SQLite, React/TypeScript

---

## Task 1: Filter out non-ready examples from report generation

The `loadExamples` method in `report_generator.go` calls `exampleRepo.List` directly (bypassing `ExampleStore`) and doesn't filter by status. Processing/failed examples with empty content leak into GPT prompts.

**Files:**
- Modify: `backend/repo_example.go`
- Modify: `backend/report_generator.go`

**Step 1: Add a `ListReady` method to `ReportExampleRepo`**

The existing `List` must continue returning all statuses (the API endpoint needs them for processing/failed indicators). Add a separate method for report generation:

```go
// ListReady returns only 'ready' report examples for a user (for report generation).
func (r *ReportExampleRepo) ListReady(ctx context.Context, userID string) ([]DBReportExample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, content, status, file_path, created_at
		FROM report_examples WHERE user_id = ? AND status = 'ready'
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list ready report examples: %w", err)
	}
	defer rows.Close()

	var result []DBReportExample
	for rows.Next() {
		var e DBReportExample
		if err := rows.Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan report example: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
```

**Step 2: Update `loadExamples` in `report_generator.go`**

Change:
```go
dbExamples, err := g.exampleRepo.List(ctx, userID)
```
to:
```go
dbExamples, err := g.exampleRepo.ListReady(ctx, userID)
```

Also update the mapping to include `Status`:
```go
examples[i] = ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status}
```

**Step 3: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 4: Commit**

```bash
git add backend/repo_example.go backend/report_generator.go
git commit -m "fix: filter out non-ready examples from report generation prompts"
```

---

## Task 2: Clean up orphaned DB rows on queue publish failure

If `queue.Publish` fails after `CreatePendingExample`, a "processing" row is left in the DB forever. Same issue in both `dispatchExtraction` and `handleDriveImportExample`.

**Files:**
- Modify: `backend/report_examples_handler.go`
- Modify: `backend/drive_import_example.go`

**Step 1: Delete DB row on publish failure in `dispatchExtraction`**

In `backend/report_examples_handler.go`, update `dispatchExtraction` — after `queue.Publish` fails, also delete the example row:

```go
	queue, err := serviceDeps.GetExtractionQueue()
	if err != nil {
		os.Remove(diskPath)
		// Clean up orphaned DB row.
		_ = store.DeleteExample(r.Context(), userID, example.ID)
		return nil, err
	}
	if err := queue.Publish(r.Context(), ExtractionJob{...}); err != nil {
		os.Remove(diskPath)
		_ = store.DeleteExample(r.Context(), userID, example.ID)
		return nil, err
	}
```

**Step 2: Same fix in `handleDriveImportExample`**

In `backend/drive_import_example.go`, after `queue.Publish` failure:

```go
	if err := queue.Publish(ctx, ExtractionJob{...}); err != nil {
		os.Remove(diskPath)
		_ = store.DeleteExample(ctx, userID, example.ID)
		log.Error(...)
		writeJSON(...)
		return
	}
```

Also after `GetExtractionQueue` failure:

```go
	queue, err := serviceDeps.GetExtractionQueue()
	if err != nil {
		os.Remove(diskPath)
		_ = store.DeleteExample(ctx, userID, example.ID)
		log.Error(...)
		writeJSON(...)
		return
	}
```

**Step 3: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 4: Commit**

```bash
git add backend/report_examples_handler.go backend/drive_import_example.go
git commit -m "fix: clean up orphaned DB rows on extraction queue publish failure"
```

---

## Task 3: Clean up files on disk when deleting a processing/failed example

When a user deletes a "processing" or "failed" example, the file on disk is orphaned. The delete handler should check for `file_path` and remove it.

**Files:**
- Modify: `backend/report_examples.go` (add `FilePath` to `ReportExample`, or use repo directly)
- Modify: `backend/repo_example.go` (return file_path on delete, or add a `GetFilePath` method)
- Modify: `backend/report_examples_handler.go`

**Step 1: Add `GetFilePath` to the repo**

In `backend/repo_example.go`:

```go
// GetFilePath returns the file_path for a report example (empty if none).
func (r *ReportExampleRepo) GetFilePath(ctx context.Context, userID string, id int64) (string, error) {
	var fp string
	err := r.db.QueryRowContext(ctx,
		"SELECT file_path FROM report_examples WHERE id = ? AND user_id = ?",
		id, userID).Scan(&fp)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get file path: %w", err)
	}
	return fp, nil
}
```

**Step 2: Update `handleDeleteReportExample` to clean up the file**

In `backend/report_examples_handler.go`, before the `store.DeleteExample` call:

```go
	// Clean up file on disk if present.
	exampleRepo := serviceDeps.GetExampleRepo()
	if fp, err := exampleRepo.GetFilePath(r.Context(), userID, req.ID); err == nil && fp != "" {
		os.Remove(fp)
	}

	store := serviceDeps.GetExampleStore()
	if err := store.DeleteExample(r.Context(), userID, req.ID); err != nil {
```

**Step 3: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 4: Commit**

```bash
git add backend/repo_example.go backend/report_examples_handler.go
git commit -m "fix: clean up files on disk when deleting processing/failed examples"
```

---

## Task 4: Add handler test for async upload path

The `handleUploadReportExample` multipart PDF upload → async dispatch path has no handler-level test.

**Files:**
- Modify: `backend/report_examples_handler_test.go`

**Step 1: Write the test**

Add to `backend/report_examples_handler_test.go`:

```go
func TestUploadExample_PDFDispatchesAsync(t *testing.T) {
	queue := newStubExtractionQueue()
	store := &stubExampleStore{}
	tmpDir := t.TempDir()
	withDeps(t, &mockDepsAll{
		exampleStore:    store,
		extractionQueue: queue,
		uploadsDir:      tmpDir,
	})

	// Build multipart form with a PDF file.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("fake pdf data"))
	writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/report-examples", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUploadReportExample(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result ReportExample
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "processing" {
		t.Errorf("status = %q, want processing", result.Status)
	}
	if len(queue.published) != 1 {
		t.Fatalf("published jobs = %d, want 1", len(queue.published))
	}
	if queue.published[0].FileName != "report.pdf" {
		t.Errorf("job filename = %q, want report.pdf", queue.published[0].FileName)
	}
}

func TestUploadExample_TextFileStoresDirect(t *testing.T) {
	store := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: store})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("Some report card text"))
	writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/report-examples", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUploadReportExample(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.uploadedContent != "Some report card text" {
		t.Errorf("content = %q, want direct text", store.uploadedContent)
	}
}
```

Add `"mime/multipart"` to the imports.

**Step 2: Run tests**

```bash
cd backend && go test -v -count=1 -run "TestUploadExample_PDF|TestUploadExample_TextFile" ./...
```

**Step 3: Commit**

```bash
git add backend/report_examples_handler_test.go
git commit -m "test: add handler tests for async PDF upload and sync text upload"
```

---

## Task 5: Propagate context to `pdfToImages`

`pdfToImages` uses `context.Background()` instead of the caller's context. If the job context is cancelled, `pdftoppm` keeps running.

**Files:**
- Modify: `backend/report_example_extractor.go`
- Modify: `backend/report_example_extractor_test.go`

**Step 1: Add `ctx` parameter to `pdfToImages`**

Change signature:
```go
func pdfToImages(ctx context.Context, data []byte) ([][]byte, error) {
```

Update the `exec.CommandContext` call (already uses `context.Background()` — change to `ctx`):
```go
cmd := exec.CommandContext(ctx, "pdftoppm", "-jpeg", "-r", "150", pdfPath, outPrefix)
```

**Step 2: Update the caller in `extractFromPDF`**

```go
images, err := pdfToImages(ctx, data)
```

**Step 3: Update tests**

In `report_example_extractor_test.go`, update calls:
```go
images, err := pdfToImages(context.Background(), data)
// ...
_, err := pdfToImages(context.Background(), []byte("not a pdf"))
```

Add `"context"` to the test file imports.

**Step 4: Fix doc comment**

```go
// pdfToImages converts PDF bytes to a slice of JPEG images (one per page)
// by shelling out to pdftoppm. Requires poppler-utils.
```

**Step 5: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 6: Commit**

```bash
git add backend/report_example_extractor.go backend/report_example_extractor_test.go
git commit -m "fix: propagate context to pdfToImages, fix stale doc comment"
```

---

## Task 6: Fix frontend drop zone spinner text

The drop zone still says "Extracting text…" during upload, but extraction is now async. Should say "Uploading…".

**Files:**
- Modify: `frontend/src/components/ReportExamples.tsx`

**Step 1: Update the spinner text**

Change:
```tsx
{driveImporting ? 'Importing from Drive…' : 'Extracting text…'}
```
to:
```tsx
{driveImporting ? 'Importing from Drive…' : 'Uploading…'}
```

**Step 2: Commit**

```bash
git add frontend/src/components/ReportExamples.tsx
git commit -m "fix: update drop zone spinner text for async extraction"
```

---

## Open Questions

- None — all fixes are straightforward.
