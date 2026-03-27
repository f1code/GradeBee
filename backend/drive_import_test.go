package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDriveImport_MissingFileID(t *testing.T) {
	body, err := json.Marshal(map[string]string{"fileName": "test.m4a"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleDriveImport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", rec.Code)
	}
}

func TestHandleDriveImport_MissingFileName(t *testing.T) {
	body, err := json.Marshal(map[string]string{"fileId": "file123"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleDriveImport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", rec.Code)
	}
}

func TestHandleDriveImport_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	handleDriveImport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400", rec.Code)
	}
}
