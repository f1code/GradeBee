// report_example_extractor.go extracts text from PDF/image report card examples
// using GPT Vision (gpt-4o-mini).
package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// ExampleExtractor extracts text content from uploaded report card files.
type ExampleExtractor interface {
	ExtractText(ctx context.Context, filename string, data []byte) (string, error)
}

// gptExampleExtractor uses OpenAI GPT-4o-mini to extract text via vision.
type gptExampleExtractor struct {
	client *openai.Client
}

func newGPTExampleExtractor() (*gptExampleExtractor, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &gptExampleExtractor{client: openai.NewClient(key)}, nil
}

const extractPrompt = "Extract all text from this report card exactly as written. Preserve the structure and formatting using plain text. Do not add commentary or explanation — only output the extracted text."

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
		text, err := e.extractFromImage(ctx, pdfToImagesMediaType, img)
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
							Detail: openai.ImageURLDetailLow,
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

// pdfToImages converts PDF bytes to a slice of PNG images (one per page)
// by shelling out to pdftoppm. Requires poppler-utils.
func pdfToImages(data []byte) ([][]byte, error) {
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
	cmd := exec.CommandContext(context.Background(), "pdftoppm", "-jpeg", "-r", "150", pdfPath, outPrefix)
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

// fileExtToMediaType maps file extensions to MIME types for GPT vision.
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

// isExtractableFile returns true if the file needs GPT extraction (PDF/image).
func isExtractableFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".pdf" {
		return true
	}
	return fileExtToMediaType(ext) != ""
}
