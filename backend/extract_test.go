package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHandleExtract_Success(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()

	serviceDeps = &mockDepsAll{
		roster: &stubRoster{
			students: []classGroup{{Name: "5A", Students: []student{{Name: "Emma Johnson"}}}},
		},
		extractor: &stubExtractor{
			result: &ExtractResponse{
				Students: []MatchedStudent{{Name: "Emma Johnson", Class: "5A", Summary: "Great work", Confidence: 0.95}},
				Date:     "2026-03-19",
			},
		},
	}

	body := mustMarshal(t, extractRequest{Transcript: "Emma did great today", FileID: "file-1"})
	req := httptest.NewRequest(http.MethodPost, "/extract", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleExtract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ExtractResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Students) != 1 || resp.Students[0].Name != "Emma Johnson" {
		t.Fatalf("unexpected students: %+v", resp.Students)
	}
}

func TestHandleExtract_MissingFields(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()
	serviceDeps = &mockDepsAll{}

	tests := []struct {
		name string
		body string
	}{
		{"empty body", "{}"},
		{"missing fileId", `{"transcript":"hello"}`},
		{"missing transcript", `{"fileId":"f1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/extract", bytes.NewBufferString(tc.body))
			w := httptest.NewRecorder()
			handleExtract(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestHandleExtract_ExtractorError(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()

	serviceDeps = &mockDepsAll{
		roster:    &stubRoster{students: []classGroup{}},
		extractor: &stubExtractor{err: fmt.Errorf("openai down")},
	}

	body := mustMarshal(t, extractRequest{Transcript: "hello", FileID: "f1"})
	req := httptest.NewRequest(http.MethodPost, "/extract", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleExtract(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleExtract_ExtractorInitError(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()

	serviceDeps = &mockDepsAll{
		roster:     &stubRoster{students: []classGroup{}},
		extractErr: fmt.Errorf("no api key"),
	}

	body := mustMarshal(t, extractRequest{Transcript: "hello", FileID: "f1"})
	req := httptest.NewRequest(http.MethodPost, "/extract", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleExtract(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleExtract_RosterUnavailable(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()

	serviceDeps = &mockDepsAll{
		rosterErr: fmt.Errorf("no spreadsheet"),
		extractor: &stubExtractor{
			result: &ExtractResponse{
				Students: []MatchedStudent{{Name: "Unknown", Class: "", Summary: "Spoke well", Confidence: 0.3}},
				Date:     "2026-03-19",
			},
		},
	}

	body := mustMarshal(t, extractRequest{Transcript: "student spoke well", FileID: "f1"})
	req := httptest.NewRequest(http.MethodPost, "/extract", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleExtract(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful degradation), got %d: %s", w.Code, w.Body.String())
	}
}
