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

func TestPdfToImages_InvalidData(t *testing.T) {
	_, err := pdfToImages([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error for invalid PDF data")
	}
}
