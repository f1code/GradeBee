package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCreateNotes_MissingFields(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()
	serviceDeps = &mockDepsAll{}

	tests := []struct {
		name string
		body createNotesRequest
	}{
		{"no students", createNotesRequest{Transcript: "T", Date: "2026-01-01"}},
		{"no transcript", createNotesRequest{Students: []noteStudentInput{{Name: "A", Class: "B", Summary: "C"}}, Date: "2026-01-01"}},
		{"no date", createNotesRequest{Students: []noteStudentInput{{Name: "A", Class: "B", Summary: "C"}}, Transcript: "T"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/notes", bytes.NewReader(body))
			w := httptest.NewRecorder()
			handleCreateNotes(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleCreateNotes_InvalidJSON(t *testing.T) {
	orig := serviceDeps
	defer func() { serviceDeps = orig }()
	serviceDeps = &mockDepsAll{}

	req := httptest.NewRequest(http.MethodPost, "/notes", bytes.NewBufferString("{invalid"))
	w := httptest.NewRecorder()
	handleCreateNotes(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStubNoteCreator_MultipleStudents(t *testing.T) {
	nc := &stubNoteCreator{
		results: []*CreateNoteResponse{
			{DocID: "doc-1", DocURL: "url-1"},
			{DocID: "doc-2", DocURL: "url-2"},
		},
	}

	ctx := context.Background()
	r1, err := nc.CreateNote(ctx, CreateNoteRequest{StudentName: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.DocID != "doc-1" {
		t.Fatalf("expected doc-1, got %s", r1.DocID)
	}

	r2, err := nc.CreateNote(ctx, CreateNoteRequest{StudentName: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.DocID != "doc-2" {
		t.Fatalf("expected doc-2, got %s", r2.DocID)
	}

	if len(nc.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(nc.calls))
	}
}

func TestStubNoteCreator_Error(t *testing.T) {
	nc := &stubNoteCreator{err: fmt.Errorf("drive error")}
	_, err := nc.CreateNote(context.Background(), CreateNoteRequest{StudentName: "Alice"})
	if err == nil {
		t.Fatal("expected error")
	}
}
