// report_example_extractor.go extracts text from PDF/image report card examples
// using GPT Vision (gpt-4o-mini).
package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
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
	mediaType := fileExtToMediaType(ext)
	if mediaType == "" {
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}

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

// fileExtToMediaType maps file extensions to MIME types for GPT vision.
func fileExtToMediaType(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
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
	return fileExtToMediaType(ext) != ""
}
