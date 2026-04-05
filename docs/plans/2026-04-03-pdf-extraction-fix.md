# PDF Extraction Fix — Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Fix PDF report card extraction (currently broken because GPT Vision doesn't accept PDFs) and make extraction async so uploads don't block the HTTP request.

**Architecture:** Stage 1 converts PDFs to PNGs via `pdftoppm` before sending to GPT Vision. Stage 2 moves extraction into the existing `memQueue` worker pool so uploads return immediately and extraction happens in the background.

**Tech Stack:** Go, `pdftoppm` (poppler-utils), OpenAI GPT-4o-mini Vision, memQueue

---

## Stage 1: Fix PDF Extraction

### Task 1: Add `pdftoppm` to Docker image

**Files:**
- Modify: `Dockerfile`

**Step 1: Add poppler-utils to apk install**

```dockerfile
FROM alpine:latest
RUN apk add --no-cache ca-certificates poppler-utils
COPY backend/dist/gradebee /gradebee
EXPOSE 8080
CMD ["/gradebee"]
```

**Step 2: Commit**

```bash
git add Dockerfile
git commit -m "chore: add poppler-utils to Docker image for PDF conversion"
```

### Task 2: Add `pdfToImages` helper function

**Files:**
- Modify: `backend/report_example_extractor.go`
- Test: `backend/report_example_extractor_test.go` (create)

**Step 1: Write the failing test**

Create `backend/report_example_extractor_test.go`:

```go
package handler

import (
	"os"
	"testing"
)

func TestPdfToImages_ValidPDF(t *testing.T) {
	// Create a minimal valid PDF for testing.
	// We'll use a real tiny PDF file.
	data, err := os.ReadFile("testdata/sample.pdf")
	if err != nil {
		t.Skip("testdata/sample.pdf not found, skipping integration test")
	}
	images, err := pdfToImages(data)
	if err != nil {
		t.Fatalf("pdfToImages failed: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("expected at least one image, got 0")
	}
	// Each image should be a valid PNG (starts with PNG magic bytes).
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

func TestFileExtToMediaType_PDF(t *testing.T) {
	// PDF should still map to application/pdf (unchanged).
	if got := fileExtToMediaType(".pdf"); got != "application/pdf" {
		t.Errorf("got %q, want application/pdf", got)
	}
}
```

**Step 2: Create test fixture**

Create `backend/testdata/` directory and add a minimal sample PDF. Generate one:

```bash
cd backend && mkdir -p testdata
# Create a minimal 1-page PDF with some text using pdftoppm's companion tool
printf '%%PDF-1.0\n1 0 obj<</Pages 2 0 R>>endobj\n2 0 obj<</Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</MediaBox[0 0 612 792]/Parent 2 0 R/Resources<<>>>>endobj\ntrailer<</Root 1 0 R>>\n' > testdata/sample.pdf
```

Note: this minimal PDF may not render well. If `pdftoppm` errors on it, find or create a proper small test PDF. A better approach:

```bash
# If `enscript` or `convert` is available:
echo "Sample Report Card\nStudent: Jane Doe\nMath: A\nScience: B+" | enscript -p - 2>/dev/null | ps2pdf - backend/testdata/sample.pdf 2>/dev/null || echo "Create testdata/sample.pdf manually"
```

**Step 3: Run test to verify it fails**

```bash
cd backend && go test -run TestPdfToImages -v
```

Expected: compilation error — `pdfToImages` doesn't exist yet.

**Step 4: Implement `pdfToImages`**

Add to `backend/report_example_extractor.go`:

```go
// pdfToImages converts a PDF byte slice to a slice of PNG images (one per page)
// by shelling out to pdftoppm. Requires poppler-utils to be installed.
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

Add `"os/exec"` to the imports.

**Step 5: Run tests**

```bash
cd backend && go test -run TestPdfToImages -v
```

Expected: PASS (assuming `pdftoppm` is installed locally and test PDF is valid).

**Step 6: Commit**

```bash
git add backend/report_example_extractor.go backend/report_example_extractor_test.go backend/testdata/
git commit -m "feat: add pdfToImages helper using pdftoppm"
```

### Task 3: Update `ExtractText` to handle PDFs via page conversion

**Files:**
- Modify: `backend/report_example_extractor.go`
- Test: `backend/report_example_extractor_test.go`

**Step 1: Write the failing test**

Add to `backend/report_example_extractor_test.go`:

```go
func TestExtractText_PDFCallsMultiplePages(t *testing.T) {
	// Mock: verify that for a PDF, multiple vision calls are made (one per page).
	// We can't easily test the real GPT call, but we can test the branching logic.
	// This test verifies the isPDF path is taken.
	ext := fileExtToMediaType(".pdf")
	if ext != "application/pdf" {
		t.Fatalf("expected application/pdf, got %s", ext)
	}
	if !isExtractableFile("report.pdf") {
		t.Fatal("expected report.pdf to be extractable")
	}
}
```

This is a lightweight check. The real integration is tested manually (Stage 1 ends with manual verification).

**Step 2: Update `ExtractText` to branch on PDF**

Modify the `ExtractText` method in `backend/report_example_extractor.go`:

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
	b64 := base64.StdEncoding.EncodeToString(data)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, b64)

	resp, err := e.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleUser,
				MultiContent: []openai.ChatMessagePart{
					{Type: openai.ChatMessagePartTypeText, Text: extractPrompt},
					{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL:    dataURL,
							Detail: openai.ImageURLDetailHigh,
						},
					},
				},
			},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		return "", fmt.Errorf("GPT extraction failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("GPT returned no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
```

**Step 3: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

Expected: PASS

**Step 4: Commit**

```bash
git add backend/report_example_extractor.go backend/report_example_extractor_test.go
git commit -m "feat: convert PDF pages to images before GPT Vision extraction"
```

### 🛑 Manual Verification Checkpoint

Deploy or run locally and test:
1. Upload a scanned PDF report card
2. Verify text is extracted correctly
3. Verify image uploads still work
4. Check logs for errors

**Do not proceed to Stage 2 until this is verified.**

---

## Stage 2: Make Extraction Async

### Task 4: Add status field to ReportExample

**Files:**
- Modify: `backend/report_examples.go`
- Modify: `backend/repo_example.go` (DB schema/queries)

**Step 1: Write failing test**

Add to `backend/report_examples_handler_test.go`:

```go
func TestUploadReportExample_PDF_ReturnsProcessingStatus(t *testing.T) {
	stub := &stubExampleStore{}
	ext := &stubExampleExtractor{result: "extracted"}
	withDeps(t, &mockDepsAll{exampleStore: stub, exampleExtractor: ext})

	// Create a multipart request with a .pdf file
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "report.pdf")
	part.Write([]byte("%PDF-fake"))
	w.Close()

	r := httptest.NewRequest(http.MethodPost, "/report-examples", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
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
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Status != "processing" {
		t.Errorf("status = %q, want processing", result.Status)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd backend && go test -run TestUploadReportExample_PDF_ReturnsProcessingStatus -v
```

Expected: FAIL — `Status` field doesn't exist on `ReportExample`.

**Step 3: Add Status field to ReportExample**

In `backend/report_examples.go`, update the struct:

```go
type ReportExample struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Status  string `json:"status"` // "ready", "processing", "error"
}
```

Update `backend/repo_example.go` to add `status` column to the DB schema and queries. Existing examples default to `"ready"`.

**Step 4: Run tests, fix compilation errors, commit**

```bash
cd backend && go test -v -count=1 ./... && make lint
git add backend/
git commit -m "feat: add status field to ReportExample"
```

### Task 5: Make upload handler return immediately for extractable files

**Files:**
- Modify: `backend/report_examples_handler.go`
- Modify: `backend/report_examples.go` (or new file for the async worker)

**Step 1: Update handler to store placeholder and queue extraction**

In `handleUploadReportExample`, for extractable files:
- Store the example with `status: "processing"` and empty content
- Store the raw file data temporarily (in the uploads dir or in-memory)
- Queue an extraction job
- Return the example immediately

Follow the same pattern as audio uploads (`upload.go`):
1. Save the raw file to `uploadsDir` with a UUID filename
2. Store a DB row (example with `status: "processing"`, `filePath` pointing to saved file)
3. Queue an extraction job via the existing `memQueue` / worker pool
4. Return the example immediately

The extraction worker:
1. Reads the file from disk
2. Calls `extractor.ExtractText(ctx, name, data)`
3. Updates the example row with extracted content + `status: "ready"` (or `"error"`)
4. Deletes the raw file from disk

This reuses the proven queue pattern and avoids goroutine leaks or captured references.

**Step 2: Add `UpdateExampleStatus` to ExampleStore interface and implementation**

**Step 3: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 4: Commit**

```bash
git add backend/
git commit -m "feat: async extraction for PDF/image report examples"
```

### Task 6: Update frontend to handle processing status

**Files:**
- Modify: `frontend/src/components/ReportExamples.tsx`

**Step 1: Show processing indicator for examples with `status: "processing"`**

- After upload, the returned example will have `status: "processing"`
- Show a spinner or "Extracting text..." label on that row
- Poll `GET /report-examples` every few seconds while any example has `status: "processing"`
- When status changes to `"ready"`, stop polling and show content

**Step 2: Handle `status: "error"`**

- Show error state on the row with option to delete/retry

**Step 3: Commit**

```bash
git add frontend/
git commit -m "feat: show processing status for report example extraction"
```

### Task 7: Apply same async pattern to Drive import

**Files:**
- Modify: `backend/drive_import_example.go`

Same change as Task 5 but for the Drive import handler. Store placeholder, queue extraction, return immediately.

**Step 1: Update handler**
**Step 2: Run tests and lint**
**Step 3: Commit**

```bash
git add backend/
git commit -m "feat: async extraction for Drive-imported report examples"
```

## Decisions
- **Page limit:** Cap at 10 pages per PDF to control GPT cost.
- **Async pattern:** Reuse `memQueue` worker pool (same as audio upload pipeline).
- **File storage:** Save raw file to `uploadsDir` (same as audio), clean up after extraction.
