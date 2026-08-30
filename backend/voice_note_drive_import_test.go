package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleDriveImport_MissingFileID(t *testing.T) {
	body, err := json.Marshal(map[string]string{"fileName": "test.m4a"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleDriveImport(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDriveImport_MissingFileName(t *testing.T) {
	body, err := json.Marshal(map[string]string{"fileId": "file123"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleDriveImport(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDriveImport_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/drive-import", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	handleDriveImport(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// driveImportRequest creates a POST /drive-import request with Clerk auth.
func newDriveImportReq(t *testing.T, userID, fileID, fileName string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{"fileId": fileID, "fileName": fileName})
	require.NoError(t, err)
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

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleDriveImport_GetFileMetaError(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{metaErr: fmt.Errorf("not found")},
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "audio.m4a"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleDriveImport_WrongMIMEType(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })
	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{meta: &DriveFile{MimeType: "application/pdf"}},
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "doc.pdf"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "audio", "expected audio error message")
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

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleDriveImport_HappyPath(t *testing.T) {
	old := serviceDeps
	t.Cleanup(func() { serviceDeps = old })

	dir := t.TempDir()
	db := setupTestDB(t)
	queue := newStubVoiceNoteQueue()

	serviceDeps = &mockDepsAll{
		driveClient: &stubDriveClient{
			meta: &DriveFile{MimeType: "audio/mpeg"},
			data: io.NopCloser(strings.NewReader("fake audio bytes")),
		},
		voiceNoteRepo:  &VoiceNoteRepo{db: db},
		voiceNoteQueue: queue,
		uploadsDir:     dir,
	}

	rec := httptest.NewRecorder()
	handleDriveImport(rec, newDriveImportReq(t, "u1", "fileABC", "audio.mp3"))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp DriveImportResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotZero(t, resp.UploadID, "expected non-zero uploadId")
	assert.Equal(t, "audio.mp3", resp.FileName)
	assert.Len(t, queue.published, 1, "expected 1 queued job")
}

// TestHandleDriveImport_OmitsFileName locks in ADR 0003 on the import path: the
// completion record ships to Sentry, and a teacher's own filename routinely names
// a child ("Manoe 12 sept.mp3"). Asserting the record is still emitted keeps the
// absence assertion from passing just because the handler bailed out early, and
// asserting on the name *value* also catches a filename interpolated into the
// message. The response body is the teacher's copy and keeps the filename.
func TestHandleDriveImport_OmitsFileName(t *testing.T) {
	for _, tc := range recordingNames {
		t.Run(tc.desc, func(t *testing.T) {
			old := serviceDeps
			t.Cleanup(func() { serviceDeps = old })

			// audio/mpeg falls back to .mp3, which no case's filename supplies.
			wantExt := tc.wantExt
			if wantExt == "" {
				wantExt = ".mp3"
			}

			queue := newStubVoiceNoteQueue()
			serviceDeps = &mockDepsAll{
				driveClient: &stubDriveClient{
					meta: &DriveFile{MimeType: "audio/mpeg"},
					data: io.NopCloser(strings.NewReader("fake audio bytes")),
				},
				voiceNoteRepo:  &VoiceNoteRepo{db: setupTestDB(t)},
				voiceNoteQueue: queue,
				uploadsDir:     t.TempDir(),
			}

			req := newDriveImportReq(t, "u1", "fileABC", tc.fileName)
			ctx, logs := captureLogs(req.Context())
			rec := httptest.NewRecorder()
			handleDriveImport(rec, req.WithContext(ctx))

			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
			var resp DriveImportResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

			out := logs.String()
			require.Contains(t, out, "drive-import completed", "completion was not logged at all")
			assert.NotContains(t, strings.ToLower(out), "manoe", "drive import leaked a recording's filename into the logs")

			done := logRecord(t, out, "drive-import completed")
			assert.Contains(t, done, `"file_ext":"`+wantExt+`"`, "completion should carry the extension that replaced the filename")
			// Closed with the trailing comma so "upload_id":1 cannot match "upload_id":12.
			assert.Contains(t, done, `"upload_id":`+strconv.FormatInt(resp.UploadID, 10)+`,`, "completion should carry the id the caller was handed")

			// See the upload test: the same extension is concatenated into the
			// on-disk name, and that path reaches its own log sites.
			require.Len(t, queue.published, 1, "expected 1 queued job")
			assert.NotContains(t, strings.ToLower(queue.published[0].FilePath), "manoe", "disk path leaked the filename's stem")
			assert.True(t, strings.HasSuffix(queue.published[0].FilePath, wantExt), "disk path should end in the validated extension, got %q", queue.published[0].FilePath)

			// The teacher's copy is a separate string from the telemetry copy: they
			// recognise the import by the name they gave it.
			assert.Contains(t, rec.Body.String(), tc.fileName, "response body should still carry the filename")
		})
	}
}
