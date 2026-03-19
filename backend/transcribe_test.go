package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTranscribe_MissingFileID(t *testing.T) {
	body, err := json.Marshal(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/transcribe", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleTranscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", rec.Code)
	}
}

func TestHandleTranscribe_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/transcribe", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	handleTranscribe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", rec.Code)
	}
}

func makeTranscribeReq(fileID string) *http.Request {
	body, err := json.Marshal(map[string]string{"fileId": fileID})
	if err != nil {
		panic(err)
	}
	return httptest.NewRequest(http.MethodPost, "/transcribe", bytes.NewReader(body))
}

func TestHandleTranscribe_HappyPathWithClassNames(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	tr := &stubTranscriber{result: "Hello class"}
	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(bytes.NewReader([]byte("audio-bytes"))),
			fileName:     "test.webm",
		},
		roster: &stubRoster{
			classNames: []string{"5A", "5B"},
		},
		transcriber: tr,
	}

	req := makeTranscribeReq("file123")
	rec := httptest.NewRecorder()
	handleTranscribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if tr.gotPrompt != "Classes: 5A, 5B" {
		t.Errorf("prompt = %q, want %q", tr.gotPrompt, "Classes: 5A, 5B")
	}
	var resp transcribeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Transcript != "Hello class" {
		t.Errorf("transcript = %q", resp.Transcript)
	}
}

func TestHandleTranscribe_RosterUnavailable(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	tr := &stubTranscriber{result: "transcript ok"}
	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(bytes.NewReader([]byte("audio"))),
			fileName:     "test.webm",
		},
		rosterErr:   fmt.Errorf("roster broken"),
		transcriber: tr,
	}

	req := makeTranscribeReq("file123")
	rec := httptest.NewRecorder()
	handleTranscribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	if tr.gotPrompt != "" {
		t.Errorf("prompt = %q, want empty", tr.gotPrompt)
	}
}

func TestHandleTranscribe_RosterNoClasses(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	tr := &stubTranscriber{result: "transcript ok"}
	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(bytes.NewReader([]byte("audio"))),
			fileName:     "test.webm",
		},
		roster:      &stubRoster{classNames: []string{}},
		transcriber: tr,
	}

	req := makeTranscribeReq("file123")
	rec := httptest.NewRecorder()
	handleTranscribe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	if tr.gotPrompt != "" {
		t.Errorf("prompt = %q, want empty", tr.gotPrompt)
	}
}

func TestHandleTranscribe_DriveDownloadFails(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadErr: fmt.Errorf("not found"),
		},
		transcriber: &stubTranscriber{},
	}

	req := makeTranscribeReq("file123")
	rec := httptest.NewRecorder()
	handleTranscribe(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", rec.Code)
	}
}

func TestHandleTranscribe_TranscriberFails(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			downloadBody: io.NopCloser(bytes.NewReader([]byte("audio"))),
			fileName:     "test.webm",
		},
		roster:      &stubRoster{},
		transcriber: &stubTranscriber{err: fmt.Errorf("whisper down")},
	}

	req := makeTranscribeReq("file123")
	rec := httptest.NewRecorder()
	handleTranscribe(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", rec.Code)
	}
}
