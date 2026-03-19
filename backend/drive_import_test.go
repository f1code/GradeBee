package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeDriveImportReq(fileID, fileName string) *http.Request {
	body, err := json.Marshal(map[string]string{"fileId": fileID, "fileName": fileName})
	if err != nil {
		panic(err)
	}
	return httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader(body))
}

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

func TestHandleDriveImport_FileNotAccessible(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			mimeTypeErr: fmt.Errorf("file not found"),
		},
	}

	req := makeDriveImportReq("file123", "test.m4a")
	rec := httptest.NewRecorder()
	handleDriveImport(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDriveImport_NotAudioFile(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		driveStore: &stubDriveStore{
			mimeType: "application/pdf",
		},
	}

	req := makeDriveImportReq("file123", "report.pdf")
	rec := httptest.NewRecorder()
	handleDriveImport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDriveImport_GoogleServicesFails(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		googleSvcErr: &apiError{Status: http.StatusForbidden, Code: "unauthorized", Message: "no session"},
	}

	req := makeDriveImportReq("file123", "test.m4a")
	rec := httptest.NewRecorder()
	handleDriveImport(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("got status %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}
