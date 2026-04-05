package handler

import (
	"os"
	"testing"
)

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
		if got := isExtractableFile(tc.name); got != tc.want {
			t.Errorf("isExtractableFile(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPdfToImages_InvalidData(t *testing.T) {
	_, err := pdfToImages([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error for invalid PDF data")
	}
}
