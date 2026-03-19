# PDF/Image Report Card Example Upload

## Goal

Allow teachers to upload report card examples as PDFs or images (in addition to existing text files). The backend uses GPT Vision to extract text content from these files before storing the plain-text result in Drive.

## Current State

- `POST /report-examples` accepts multipart (text file) or JSON (pasted text)
- Files stored as plain text in Drive's `report-examples/` folder
- Frontend (`ReportExamples.tsx`) has drag-and-drop file upload, currently accepts any file but treats content as raw text via `string(data)`

## Proposed Changes

### Backend

1. **`report_examples_handler.go`** — Update `handleUploadReportExample`:
   - Detect file type from extension/content-type (`.pdf`, `.png`, `.jpg`, `.jpeg`, `.webp`)
   - For text files: keep current behavior
   - For PDF/image files: call a new `ExampleExtractor` to get text via GPT
   - Store extracted text (not the original binary) in Drive as before

2. **`report_example_extractor.go`** (new file) — GPT Vision-based text extraction:
   - Interface: `ExampleExtractor` with `ExtractText(ctx, filename string, data []byte) (string, error)`
   - Implementation: `gptExampleExtractor` using OpenAI GPT-4o
   - For **images**: send as base64 image content to gpt-5.4-mini with prompt "Extract all text from this report card exactly as written"
   - For **PDFs**: send PDF as base64 to gpt-5.4-mini (supports PDF input natively); concatenate all pages into one result

3. **`deps.go`** — Add `GetExampleExtractor() ExampleExtractor` to `deps` interface

4. **`audio_format.go`** or new **`file_detect.go`** — Helper to detect if a file is PDF/image based on magic bytes or extension

### Frontend

5. **`ReportExamples.tsx`** — Update file input to accept specific types:
   - Add `accept=".txt,.pdf,.png,.jpg,.jpeg,.webp"` to file input
   - Show a processing/extracting spinner (upload may take longer for PDFs)
   - Display "(extracted from PDF)" or similar badge on examples that came from non-text sources

6. **`api.ts`** — No changes needed (already sends multipart)

## Decisions

1. **Always use GPT Vision** (gpt-5.4-mini) for extraction — no Go PDF libraries
2. **Multi-page PDFs**: concatenate all pages into one example
3. **Don't store originals** — only extracted text in Drive
4. **10MB file size limit** is sufficient
