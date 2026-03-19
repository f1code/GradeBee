package handler

import (
	"bytes"
	"encoding/json"
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
