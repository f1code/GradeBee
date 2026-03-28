package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
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

// driveImportRequest creates a POST /drive-import request with Clerk auth.
func newDriveImportReq(t *testing.T, userID, fileID, fileName string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{"fileId": fileID, "fileName": fileName})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
	return r.WithContext(ctx)
}

func TestHandleDriveImport_GetDriveClientError(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = &mockDepsAll{
		driveClientErr: fmt.Errorf("oauth token expired"),
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "audio.m4a"))

	if rec.Code != http.StatusBadGateway {
		t.Errorf("got %d, want 502", rec.Code)
	}
}

func TestHandleDriveImport_GetFileMetaError(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{metaErr: fmt.Errorf("not found")},
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "audio.m4a"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestHandleDriveImport_WrongMIMEType(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{meta: &DriveFile{MimeType: "application/pdf"}},
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "doc.pdf"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "audio") {
		t.Errorf("expected audio error message, got: %s", rec.Body.String())
	}
}

func TestHandleDriveImport_DownloadFileError(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{
			meta:  &DriveFile{MimeType: "audio/mpeg"},
			dlErr: fmt.Errorf("download failed"),
		},
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "audio.mp3"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rec.Code)
	}
}

func TestHandleDriveImport_HappyPath(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })

	dir := t.TempDir()
	db := setupTestDB(t)
	queue := newStubUploadQueue()

	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "audio/mpeg"},
			data: io.NopCloser(strings.NewReader("fake audio bytes")),
		},
		uploadRepo:  &UploadRepo{db: db},
		uploadQueue: queue,
		uploadsDir:  dir,
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "audio.mp3"))

	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp driveImportResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.UploadID == 0 {
		t.Error("expected non-zero uploadId")
	}
	if resp.FileName != "audio.mp3" {
		t.Errorf("got fileName %q, want %q", resp.FileName, "audio.mp3")
	}
	if len(queue.published) != 1 {
		t.Errorf("expected 1 queued job, got %d", len(queue.published))
	}
}
