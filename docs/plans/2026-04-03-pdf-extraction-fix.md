# PDF Extraction Fix — Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Fix PDF report card extraction (currently broken because GPT Vision doesn't accept PDFs) and make extraction async so uploads don't block the HTTP request.

**Architecture:** Stage 1 converts PDFs to PNGs via `pdftoppm` before sending to GPT Vision. Stage 2 introduces an `ExtractionJob` type with its own `JobQueue[ExtractionJob]` (following the established `MemQueue` pattern from voice notes) so uploads return immediately and extraction happens in the background.

**Tech Stack:** Go, `pdftoppm` (poppler-utils), OpenAI GPT-4o-mini Vision, `MemQueue[ExtractionJob]`

---

## Stage 1: Fix PDF Extraction

### Task 1: Add `pdftoppm` to Docker image

**Files:**
- Modify: `Dockerfile`

**Step 1: Add poppler-utils to apk install**

Change:
```dockerfile
RUN apk add --no-cache ca-certificates
```
to:
```dockerfile
RUN apk add --no-cache ca-certificates poppler-utils
```

**Step 2: Commit**

```bash
git add Dockerfile
git commit -m "chore: add poppler-utils to Docker image for PDF conversion"
```

### Task 2: Add `pdfToImages` helper and split `ExtractText` for PDF vs image

**Files:**
- Modify: `backend/report_example_extractor.go`
- Create: `backend/report_example_extractor_test.go`

**Step 1: Write tests**

Create `backend/report_example_extractor_test.go`:

```go
func TestPdfToImages_ValidPDF(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.pdf")
	if err != nil {
		t.Skip("testdata/sample.pdf not found, skipping")
	}
	images, err := pdfToImages(data)
	if err != nil {
		t.Fatalf("pdfToImages failed: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("expected at least one image")
	}
	for i, img := range images {
		if len(img) < 8 || string(img[:4]) != "\x89PNG" {
			t.Errorf("image %d is not a valid PNG", i)
		}
	}
}

func TestPdfToImages_InvalidData(t *testing.T) {
	_, err := pdfToImages([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error for invalid PDF data")
	}
}
```

Create `backend/testdata/sample.pdf` — a minimal valid PDF for testing.

**Step 2: Implement `pdfToImages`**

Add to `backend/report_example_extractor.go`:

```go
// pdfToImages converts PDF bytes to a slice of PNG images (one per page)
// by shelling out to pdftoppm. Requires poppler-utils.
func pdfToImages(data []byte) ([][]byte, error) {
	tmpDir, err := os.MkdirTemp("", "pdf-extract-*")
	if err != nil {
		return nil, fmt.Errorf("pdfToImages: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "input.pdf")
	if err := os.WriteFile(pdfPath, data, 0600); err != nil {
		return nil, fmt.Errorf("pdfToImages: write temp PDF: %w", err)
	}

	outPrefix := filepath.Join(tmpDir, "page")
	cmd := exec.CommandContext(context.Background(), "pdftoppm", "-png", "-r", "200", pdfPath, outPrefix)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdfToImages: pdftoppm failed: %w\nOutput: %s", err, string(output))
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("pdfToImages: read output dir: %w", err)
	}

	var images [][]byte
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".png" {
			continue
		}
		img, err := os.ReadFile(filepath.Join(tmpDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("pdfToImages: read page image: %w", err)
		}
		images = append(images, img)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("pdfToImages: no pages extracted")
	}
	return images, nil
}
```

**Step 3: Refactor `ExtractText` into `extractFromPDF` + `extractFromImage`**

Split the existing monolithic `ExtractText` method:

```go
func (e *gptExampleExtractor) ExtractText(ctx context.Context, filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".pdf" {
		return e.extractFromPDF(ctx, data)
	}
	mediaType := fileExtToMediaType(ext)
	if mediaType == "" {
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
	return e.extractFromImage(ctx, mediaType, data)
}

func (e *gptExampleExtractor) extractFromPDF(ctx context.Context, data []byte) (string, error) {
	images, err := pdfToImages(data)
	if err != nil {
		return "", fmt.Errorf("PDF conversion failed: %w", err)
	}
	const maxPages = 10
	if len(images) > maxPages {
		images = images[:maxPages]
	}
	var parts []string
	for i, img := range images {
		text, err := e.extractFromImage(ctx, "image/png", img)
		if err != nil {
			return "", fmt.Errorf("extraction failed on page %d: %w", i+1, err)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

func (e *gptExampleExtractor) extractFromImage(ctx context.Context, mediaType string, data []byte) (string, error) {
	// ... existing GPT Vision call logic (moved from ExtractText) ...
}
```

Remove `"application/pdf"` from `fileExtToMediaType` — PDFs are no longer sent directly to Vision.

**Step 4: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 5: Commit**

```bash
git add backend/report_example_extractor.go backend/report_example_extractor_test.go backend/testdata/
git commit -m "feat: convert PDF pages to images before GPT Vision extraction"
```

### 🛑 Manual Verification Checkpoint (Stage 1)

1. Run full test suite: `cd backend && go test -v -count=1 ./...`
2. Run locally, upload a scanned PDF report card, verify text is extracted
3. Verify image uploads still work
4. Check logs for errors

**Do not proceed to Stage 2 until this is verified.**

---

## Stage 2: Make Extraction Async

### Task 3: Add `status` and `file_path` columns to `report_examples`

**Files:**
- Create: `backend/sql/003_example_status.sql`
- Modify: `backend/repo_example.go` (update queries to include `status`, `file_path`)
- Modify: `backend/report_examples.go` (add `Status` field to `ReportExample`, update `ExampleStore` and `dbExampleStore`)

**Step 1: Create migration**

```sql
-- 003_example_status.sql
ALTER TABLE report_examples ADD COLUMN status TEXT NOT NULL DEFAULT 'ready';
ALTER TABLE report_examples ADD COLUMN file_path TEXT NOT NULL DEFAULT '';
```

Existing rows get `status='ready'` (already extracted). New async uploads start as `'processing'`.

**Step 2: Update `ReportExample` struct**

```go
type ReportExample struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Status   string `json:"status"`   // "ready", "processing", "failed"
	FilePath string `json:"-"`        // internal, not serialized to API
}
```

**Step 3: Add `UpdateExampleStatus` to `ExampleStore`**

```go
// UpdateExampleStatus sets the status, content, and error for async extraction completion.
UpdateExampleStatus(ctx context.Context, id int64, status, content string) error
```

Implement in `dbExampleStore` / `ReportExampleRepo`.

**Step 4: Update all repo queries** to scan `status` and `file_path` columns.

**Step 5: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 6: Commit**

```bash
git add backend/
git commit -m "feat: add status and file_path columns to report_examples"
```

### Task 4: Define `ExtractionJob` and wire up `MemQueue[ExtractionJob]`

**Files:**
- Create: `backend/report_example_job.go`
- Modify: `backend/deps.go` (add `GetExtractionQueue`, init function, singleton)

**Step 1: Define `ExtractionJob`**

Create `backend/report_example_job.go`:

```go
// ExtractionJob represents an async report example text extraction job.
type ExtractionJob struct {
	UserID    string    `json:"userId"`
	ExampleID int64     `json:"exampleId"`
	FilePath  string    `json:"filePath"`
	FileName  string    `json:"fileName"`
	Status    string    `json:"status"`    // reuse JobStatusQueued/Done/Failed
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func (j ExtractionJob) JobKey() string {
	return fmt.Sprintf("%s/ex-%d", j.UserID, j.ExampleID)
}

func (j ExtractionJob) OwnerID() string { return j.UserID }
```

**Step 2: Add `GetExtractionQueue` to `deps` interface and `prodDeps`**

Follow the exact pattern of `GetVoiceNoteQueue` / `InitVoiceNoteQueue`:

```go
// In deps interface:
GetExtractionQueue() (JobQueue[ExtractionJob], error)

// Singleton + init:
var extractionQueueInstance JobQueue[ExtractionJob]

func InitExtractionQueue(d deps, workers int) *MemQueue[ExtractionJob] {
	q := NewMemQueue[ExtractionJob](func(ctx context.Context, queue JobQueue[ExtractionJob], key string) error {
		return processExtraction(ctx, d, queue, key)
	}, workers)
	extractionQueueInstance = q
	return q
}
```

Call `InitExtractionQueue` at startup alongside `InitVoiceNoteQueue`.

**Step 3: Run tests (will fail until `processExtraction` exists — stub it)**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 4: Commit**

```bash
git add backend/report_example_job.go backend/deps.go
git commit -m "feat: define ExtractionJob and wire MemQueue for async extraction"
```

### Task 5: Implement `processExtraction` worker

**Files:**
- Create: `backend/report_example_process.go`
- Create: `backend/report_example_process_test.go`

**Step 1: Write tests**

Test the happy path (file exists → extract → update example to `ready`) and error path (extraction fails → example set to `failed`).

Use stub implementations of `ExampleExtractor` and `ExampleStore` (same pattern as `voice_note_process_test.go`).

**Step 2: Implement `processExtraction`**

In `backend/report_example_process.go`:

```go
// processExtraction runs the text extraction pipeline for a single job.
func processExtraction(ctx context.Context, d deps, q JobQueue[ExtractionJob], key string) error {
	log := loggerFromContext(ctx)

	job, err := q.GetJob(ctx, key)
	if err != nil {
		return fmt.Errorf("process extraction: get job: %w", err)
	}
	if job.Status != JobStatusQueued {
		return nil
	}

	fail := func(step string, err error) error {
		log.Error("process extraction failed", "step", step, "key", key, "error", err)
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		_ = q.UpdateJob(ctx, *job)
		// Also mark the example as failed.
		store := d.GetExampleStore()
		_ = store.UpdateExampleStatus(ctx, job.ExampleID, "failed", "")
		return fmt.Errorf("process extraction: %s: %w", step, err)
	}

	// Read file from disk.
	data, err := os.ReadFile(job.FilePath)
	if err != nil {
		return fail("read file", err)
	}

	// Extract text.
	extractor, err := d.GetExampleExtractor()
	if err != nil {
		return fail("init extractor", err)
	}
	content, err := extractor.ExtractText(ctx, job.FileName, data)
	if err != nil {
		return fail("extract", err)
	}

	// Update example with extracted content.
	store := d.GetExampleStore()
	if err := store.UpdateExampleStatus(ctx, job.ExampleID, "ready", content); err != nil {
		return fail("update example", err)
	}

	// Clean up file.
	os.Remove(job.FilePath)

	job.Status = JobStatusDone
	job.Error = ""
	_ = q.UpdateJob(ctx, *job)

	log.Info("extraction completed", "key", key, "example_id", job.ExampleID)
	return nil
}
```

**Step 3: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 4: Commit**

```bash
git add backend/report_example_process.go backend/report_example_process_test.go
git commit -m "feat: implement processExtraction worker for async text extraction"
```

### Task 6: Update upload/import handlers to return immediately for extractable files

**Files:**
- Modify: `backend/report_examples_handler.go`
- Modify: `backend/drive_import_example.go`

**Step 1: Update `handleUploadReportExample`**

For extractable files (PDF/image), instead of calling `ExtractText` synchronously:

1. Save the raw file to `uploadsDir` with a UUID filename
2. Create a DB row with `status: "processing"` and empty content
3. Publish an `ExtractionJob` to the extraction queue
4. Return the example immediately (with `status: "processing"`)

For text files (pasted JSON body), behavior is unchanged — store directly with `status: "ready"`.

**Step 2: Update `handleDriveImportExample`**

Same change for non-text MIME types:

1. Save downloaded Drive file to `uploadsDir`
2. Create DB row with `status: "processing"`
3. Publish `ExtractionJob`
4. Return immediately

Text MIME types (`text/plain`, `text/markdown`) still store directly.

**Step 3: Update `ExampleStore.UploadExample`** to accept an optional status parameter (or add a new method like `CreatePendingExample`).

**Step 4: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 5: Commit**

```bash
git add backend/
git commit -m "feat: async extraction for report example uploads and Drive imports"
```

### Task 7: Update frontend to handle processing status

**Files:**
- Modify: `frontend/src/components/ReportExamples.tsx`
- Modify: `frontend/src/api.ts` (if `ReportExampleItem` needs `status` field — should come from generated types)

**Step 1: Regenerate API types** to pick up the new `Status` field on `ReportExample`.

**Step 2: Show processing indicator**

- Examples with `status: "processing"` show a spinner / "Extracting text..." label
- Poll `GET /report-examples` every 3s while any example has `status: "processing"`
- Stop polling when all are `"ready"` or `"failed"`

**Step 3: Handle `status: "failed"`**

- Show error indicator with option to delete

**Step 4: Commit**

```bash
git add frontend/
git commit -m "feat: show processing status for report example extraction"
```

### 🛑 Manual Verification Checkpoint (Stage 2)

1. Run full test suite: `cd backend && go test -v -count=1 ./...`
2. Upload a PDF — should return immediately with "processing" status
3. Frontend should show spinner, then extracted content when done
4. Upload an image — same async behavior
5. Paste text — should still work synchronously (status: "ready" immediately)
6. Drive import of PDF — async
7. Drive import of text file — sync

## Open Questions
- Should we support retry for failed extractions, or just delete + re-upload?

## Decisions
- **Page limit:** Cap at 10 pages per PDF to control GPT cost.
- **Async pattern:** `JobQueue[ExtractionJob]` with `MemQueue`, matching `VoiceNoteJob` pattern exactly.
- **File storage:** Save raw file to `uploadsDir` (same as voice notes), clean up after extraction.
- **Status values:** `"ready"` (default for existing rows), `"processing"`, `"failed"`.
- **Text uploads:** Still synchronous — no need to queue a job for plain text.
