package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractionResponseSchema_Valid(t *testing.T) {
	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal(extractionResponseSchema(), &schema), "extractionResponseSchema is not valid JSON")
	assert.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok, "missing 'properties' in schema")
	assert.Contains(t, props, "success", "missing 'success' property")
	assert.Contains(t, props, "text", "missing 'text' property")
}

func TestPdfToImages_ValidPDF(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.pdf")
	if err != nil {
		t.Skip("testdata/sample.pdf not found, skipping")
	}
	images, err := pdfToImages(t.Context(), data)
	require.NoError(t, err, "pdfToImages failed")
	require.NotEmpty(t, images)
	for i, img := range images {
		// JPEG magic bytes: FF D8 FF
		assert.True(t, len(img) >= 3 && img[0] == 0xFF && img[1] == 0xD8 && img[2] == 0xFF,
			"image %d is not a valid JPEG", i)
	}
}

func TestIsExtractableFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"report.pdf", true},
		{"report.PDF", true},
		{"photo.png", true},
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"photo.webp", true},
		{"notes.txt", false},
		{"data.json", false},
		{"", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isExtractableFile(tc.name), "isExtractableFile(%q)", tc.name)
	}
}

func TestPdfToImages_InvalidData(t *testing.T) {
	_, err := pdfToImages(context.Background(), []byte("not a pdf"))
	assert.Error(t, err, "expected error for invalid PDF data")
}
