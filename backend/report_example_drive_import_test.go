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

	clerk "github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDriveImportExampleReq creates a POST /drive-import-example request with Clerk auth.
func newDriveImportExampleReq(t *testing.T, userID, fileID, fileName string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{"fileId": fileID, "fileName": fileName})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/drive-import-example", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
	return r.WithContext(ctx)
}

// noAuthDriveImportExampleReq creates a request without auth.
func noAuthDriveImportExampleReq(t *testing.T, fileID, fileName string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{"fileId": fileID, "fileName": fileName})
	require.NoError(t, err)
	return httptest.NewRequest(http.MethodPost, "/drive-import-example", bytes.NewReader(body))
}

// withDeps swaps serviceDeps for the duration of the test and restores it on cleanup.
func withDeps(t *testing.T, deps deps) {
	t.Helper()
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = deps
}

func TestDriveImportExample_InvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/drive-import-example", strings.NewReader("{invalid"))
	handleDriveImportExample(rec, r)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_MissingFileID(t *testing.T) {
	rec := httptest.NewRecorder()
	body, err := json.Marshal(map[string]string{"fileName": "report.pdf"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/drive-import-example", bytes.NewReader(body))
	handleDriveImportExample(rec, r)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_MissingFileName(t *testing.T) {
	rec := httptest.NewRecorder()
	body, err := json.Marshal(map[string]string{"fileId": "abc123"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/drive-import-example", bytes.NewReader(body))
	handleDriveImportExample(rec, r)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_BlankFileName(t *testing.T) {
	// NOTE: fileName validation occurs after the file is downloaded, so a
	// drive client returning real data is required even though we expect a 400.
	dc := &stubDriveClient{
		meta: &DriveFile{MimeType: "text/plain"},
		data: io.NopCloser(strings.NewReader("hello")),
	}
	withDeps(t, &mockDepsAll{driveClient: dc})

	rec := httptest.NewRecorder()
	r := newDriveImportExampleReq(t, "u1", "fileXYZ", "   ")
	handleDriveImportExample(rec, r)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_NoSession(t *testing.T) {
	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, noAuthDriveImportExampleReq(t, "abc", "file.txt"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDriveImportExample_DriveClientError(t *testing.T) {
	withDeps(t, &mockDepsAll{driveClientErr: fmt.Errorf("oauth expired")})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestDriveImportExample_FileMetaError(t *testing.T) {
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{metaErr: fmt.Errorf("not found")},
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDriveImportExample_DownloadError(t *testing.T) {
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta:  &DriveFile{MimeType: "application/pdf"},
			dlErr: fmt.Errorf("download failed"),
		},
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDriveImportExample_DisallowedMIME(t *testing.T) {
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{meta: &DriveFile{MimeType: "application/zip"}},
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "archive.zip"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_ExceedsSizeLimit(t *testing.T) {
	bigData := bytes.Repeat([]byte("x"), maxReportImportBytes)
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "text/plain"},
			data: io.NopCloser(bytes.NewReader(bigData)),
		},
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "big.txt"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_PDFExtractsText(t *testing.T) {
	store := &stubExampleStore{}
	queue := newStubExtractionQueue()
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "application/pdf"},
			data: io.NopCloser(bytes.NewReader([]byte{1, 2, 3})),
		},
		exampleStore:    store,
		extractionQueue: queue,
		uploadsDir:      t.TempDir(),
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	var result ReportExample
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	assert.Equal(t, "processing", result.Status)
	require.Len(t, queue.published, 1)
	assert.Equal(t, "report.pdf", queue.published[0].FileName)
}

func TestDriveImportExample_ImageExtractsText(t *testing.T) {
	store := &stubExampleStore{}
	queue := newStubExtractionQueue()
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "image/png"},
			data: io.NopCloser(bytes.NewReader([]byte{0x89, 0x50})),
		},
		exampleStore:    store,
		extractionQueue: queue,
		uploadsDir:      t.TempDir(),
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "scan.png"))
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	assert.Len(t, queue.published, 1)
}

func TestDriveImportExample_ExtractorUnavailable(t *testing.T) {
	// With async extraction, extractor unavailable is no longer tested at import time.
	// The queue handles extraction. Test queue unavailable instead.
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "application/pdf"},
			data: io.NopCloser(bytes.NewReader([]byte{1, 2})),
		},
		exampleStore:       &stubExampleStore{},
		extractionQueueErr: fmt.Errorf("not initialized"),
		uploadsDir:         t.TempDir(),
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDriveImportExample_ExtractorFails(t *testing.T) {
	// With async extraction, extractor failures happen in the worker, not at import time.
	// This test now verifies the async path dispatches successfully.
	queue := newStubExtractionQueue()
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "application/pdf"},
			data: io.NopCloser(bytes.NewReader([]byte{1, 2})),
		},
		exampleStore:    &stubExampleStore{},
		extractionQueue: queue,
		uploadsDir:      t.TempDir(),
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	assert.Equal(t, http.StatusOK, rec.Code, "want 200 (async), body = %s", rec.Body.String())
}

func TestDriveImportExample_ExtractorReturnsEmpty(t *testing.T) {
	// With async extraction, empty results are handled in the worker.
	// This test now verifies the async path dispatches successfully.
	queue := newStubExtractionQueue()
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "application/pdf"},
			data: io.NopCloser(bytes.NewReader([]byte{1, 2})),
		},
		exampleStore:    &stubExampleStore{},
		extractionQueue: queue,
		uploadsDir:      t.TempDir(),
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.pdf"))
	assert.Equal(t, http.StatusOK, rec.Code, "want 200 (async), body = %s", rec.Body.String())
}

func TestDriveImportExample_PlainTextDirect(t *testing.T) {
	ext := &stubExampleExtractor{}
	store := &stubExampleStore{}
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "text/plain"},
			data: io.NopCloser(strings.NewReader("plain text content")),
		},
		exampleExtractor: ext,
		exampleStore:     store,
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "notes.txt"))
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	assert.Empty(t, ext.gotFilename, "extractor should NOT be called for plain text")
	assert.Equal(t, "plain text content", store.uploadedContent)
}

func TestDriveImportExample_MarkdownDirect(t *testing.T) {
	ext := &stubExampleExtractor{}
	store := &stubExampleStore{}
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "text/markdown"},
			data: io.NopCloser(strings.NewReader("# Report")),
		},
		exampleExtractor: ext,
		exampleStore:     store,
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "report.md"))
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	assert.Empty(t, ext.gotFilename, "extractor should NOT be called for markdown")
}

func TestDriveImportExample_EmptyTextFile(t *testing.T) {
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "text/plain"},
			data: io.NopCloser(strings.NewReader("")),
		},
		exampleStore: &stubExampleStore{},
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "empty.txt"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriveImportExample_StoreFailure(t *testing.T) {
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "text/plain"},
			data: io.NopCloser(strings.NewReader("content")),
		},
		exampleStore: &stubExampleStore{uploadErr: fmt.Errorf("db error")},
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "file.txt"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDriveImportExample_Success(t *testing.T) {
	store := &stubExampleStore{
		uploadResult: &ReportExample{ID: 42, Name: "My Report"},
	}
	withDeps(t, &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "text/plain"},
			data: io.NopCloser(strings.NewReader("report content")),
		},
		exampleStore: store,
	})

	rec := httptest.NewRecorder()
	handleDriveImportExample(rec, newDriveImportExampleReq(t, "u1", "fileABC", "My Report"))
	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	var result ReportExample
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	assert.Equal(t, int64(42), result.ID)
	assert.Equal(t, "My Report", result.Name)
	assert.Equal(t, "report content", store.uploadedContent)
}
