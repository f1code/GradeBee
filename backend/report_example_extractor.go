// report_example_extractor.go extracts text from PDF/image report card examples
// using vision via an LLMProvider.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ExampleExtractor extracts text content from uploaded report card files.
type ExampleExtractor interface {
	ExtractText(ctx context.Context, filename string, data []byte) (string, error)
}

// llmExampleExtractor uses an LLMProvider to extract text via vision.
type llmExampleExtractor struct {
	provider LLMProvider
}

func newLLMExampleExtractor(provider LLMProvider) *llmExampleExtractor {
	return &llmExampleExtractor{provider: provider}
}

const extractPrompt = `Extract all text from this report card image exactly as written. Preserve the structure and formatting using plain text. If the image does not contain a readable report card or document, set success to false and leave text empty.`

// extractionResult is the structured response from vision extraction.
type extractionResult struct {
	Success bool   `json:"success"`
	Text    string `json:"text"`
}

// extractionResponseSchema returns the JSON schema for structured extraction output.
func extractionResponseSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"success": {"type": "boolean"},
			"text": {"type": "string"}
		},
		"required": ["success", "text"],
		"additionalProperties": false
	}`)
}

func (e *llmExampleExtractor) ExtractText(ctx context.Context, filename string, data []byte) (string, error) {
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

func (e *llmExampleExtractor) extractFromPDF(ctx context.Context, data []byte) (string, error) {
	images, err := pdfToImages(ctx, data)
	if err != nil {
		return "", fmt.Errorf("PDF conversion failed: %w", err)
	}
	const maxPages = 10
	if len(images) > maxPages {
		images = images[:maxPages]
	}

	// Extract all pages concurrently.
	type pageResult struct {
		text string
		err  error
	}
	results := make([]pageResult, len(images))
	var wg sync.WaitGroup
	for i, img := range images {
		wg.Add(1)
		go func(idx int, imgData []byte) {
			defer wg.Done()
			text, err := e.extractFromImage(ctx, pdfToImagesMediaType, imgData)
			results[idx] = pageResult{text: text, err: err}
		}(i, img)
	}
	wg.Wait()

	var parts []string
	for i, r := range results {
		if r.err != nil {
			return "", fmt.Errorf("extraction failed on page %d: %w", i+1, r.err)
		}
		parts = append(parts, r.text)
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

func (e *llmExampleExtractor) extractFromImage(ctx context.Context, mediaType string, data []byte) (string, error) {
	var result extractionResult
	_, err := e.provider.Vision(ctx, VisionRequest{
		Prompt:     extractPrompt,
		MediaType:  mediaType,
		ImageData:  data,
		SchemaName: "extraction_result",
		Schema:     extractionResponseSchema(),
	}, &result)
	if err != nil {
		return "", fmt.Errorf("vision extraction failed: %w", err)
	}
	if !result.Success {
		return "", fmt.Errorf("LLM could not extract text from image (not a readable document)")
	}
	if strings.TrimSpace(result.Text) == "" {
		return "", fmt.Errorf("LLM returned empty extraction")
	}
	return strings.TrimSpace(result.Text), nil
}

// pdfToImages converts PDF bytes to a slice of JPEG images (one per page)
// by shelling out to pdftoppm. Requires poppler-utils.
func pdfToImages(ctx context.Context, data []byte) ([][]byte, error) {
	tmpDir, err := os.MkdirTemp("", "pdf-extract-*")
	if err != nil {
		return nil, fmt.Errorf("pdfToImages: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "input.pdf")
	if err := os.WriteFile(pdfPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("pdfToImages: write temp PDF: %w", err)
	}

	outPrefix := filepath.Join(tmpDir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-jpeg", "-r", "150", pdfPath, outPrefix)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdfToImages: pdftoppm failed: %w\nOutput: %s", err, string(output))
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("pdfToImages: read output dir: %w", err)
	}

	var images [][]byte
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".jpg" {
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

// pdfToImagesMediaType is the MIME type of images produced by pdfToImages.
const pdfToImagesMediaType = "image/jpeg"

// fileExtToMediaType maps file extensions to MIME types for vision.
func fileExtToMediaType(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// isExtractableFile returns true if the file needs LLM extraction (PDF/image).
func isExtractableFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".pdf" {
		return true
	}
	return fileExtToMediaType(ext) != ""
}

// mimeToExt returns a file extension for common MIME types used by report imports.
func mimeToExt(mime string) string {
	switch mime {
	case "application/pdf":
		return ".pdf"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
