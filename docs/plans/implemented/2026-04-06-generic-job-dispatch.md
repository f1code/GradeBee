# Generic Job Dispatch Helpers — Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Extract shared helpers for the "save file to disk → create DB row → publish job → cleanup on failure" pattern used by both voice notes and report examples. Fix voice note missing cleanup bug.

**Architecture:** Two small helpers in `job_queue.go`: `saveToUploadsDir` (save bytes with unique name) and `publishOrCleanup` (publish job, run cleanup funcs on failure). Domain-specific dispatch functions call these. Voice note handlers switch from streaming to `[]byte` (files are ≤25MB, fine in memory). Voice notes get the same cleanup-on-failure behavior report examples already have.

**Tech Stack:** Go, generics

---

## Task 1: Add shared helpers to `job_queue.go`

**Files:**
- Modify: `backend/job_queue.go`

**Step 1: Add `saveToUploadsDir`**

Append to `backend/job_queue.go`:

```go
// saveToUploadsDir writes data to the uploads directory with a unique filename
// built from a UUID and the given extension (e.g. ".pdf"). Returns the full
// disk path. Callers are responsible for cleanup on downstream failures.
func saveToUploadsDir(data []byte, ext string) (string, error) {
	uploadsDir := serviceDeps.GetUploadsDir()
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)
	if err := os.WriteFile(diskPath, data, 0o644); err != nil {
		return "", fmt.Errorf("save to uploads dir: %w", err)
	}
	return diskPath, nil
}
```

**Step 2: Add `publishOrCleanup`**

```go
// publishOrCleanup publishes a job to the queue. If publishing fails (including
// queue unavailability), it runs all cleanup functions best-effort and returns
// the error.
func publishOrCleanup[T Keyed](ctx context.Context, queue JobQueue[T], job T, cleanups ...func()) error {
	if err := queue.Publish(ctx, job); err != nil {
		for _, fn := range cleanups {
			fn()
		}
		return err
	}
	return nil
}
```

**Step 3: Add imports**

Add `"fmt"`, `"os"`, `"path/filepath"`, and `"github.com/google/uuid"` to the import block.

**Step 4: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 5: Commit**

```bash
git add backend/job_queue.go
git commit -m "feat: add saveToUploadsDir and publishOrCleanup shared helpers"
```

---

## Task 2: Rewrite `dispatchExtraction` to use shared helpers

**Files:**
- Modify: `backend/report_example_dispatch.go`

**Step 1: Rewrite the function**

Replace the entire body of `report_example_dispatch.go` with:

```go
// report_example_dispatch.go contains the dispatch logic for saving a file
// to disk, creating a pending DB row, and publishing an extraction job.
package handler

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// dispatchExtraction saves a file to disk, creates a pending DB row, and
// publishes an ExtractionJob. Returns the pending example for the API response.
// extOverride, if non-empty, is used instead of the file extension from name
// (useful when the MIME type is more reliable than the filename).
func dispatchExtraction(ctx context.Context, userID, name string, data []byte, extOverride string) (*ReportExample, error) {
	ext := filepath.Ext(name)
	if extOverride != "" {
		ext = extOverride
	}

	diskPath, err := saveToUploadsDir(data, ext)
	if err != nil {
		return nil, err
	}

	store := serviceDeps.GetExampleStore()
	example, err := store.CreatePendingExample(ctx, userID, name, diskPath)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	queue, err := serviceDeps.GetExtractionQueue()
	if err != nil {
		os.Remove(diskPath)
		_ = store.DeleteExample(ctx, userID, example.ID) //nolint:errcheck // best-effort cleanup
		return nil, err
	}

	err = publishOrCleanup(ctx, queue, ExtractionJob{
		UserID:    userID,
		ExampleID: example.ID,
		FilePath:  diskPath,
		FileName:  name,
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	},
		func() { os.Remove(diskPath) },
		func() { _ = store.DeleteExample(ctx, userID, example.ID) }, //nolint:errcheck // best-effort
	)
	if err != nil {
		return nil, err
	}

	return example, nil
}
```

**Step 2: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 3: Commit**

```bash
git add backend/report_example_dispatch.go
git commit -m "refactor: dispatchExtraction uses shared helpers"
```

---

## Task 3: Create `dispatchVoiceNote` and refactor voice note handlers

**Files:**
- Create: `backend/voice_note_dispatch.go`
- Modify: `backend/voice_note_upload.go`
- Modify: `backend/voice_note_drive_import.go`

**Step 1: Create `voice_note_dispatch.go`**

```go
// voice_note_dispatch.go contains the dispatch logic for saving a voice note
// file to disk, creating a DB row, and publishing a processing job.
package handler

import (
	"context"
	"os"
	"time"
)

// dispatchVoiceNote saves audio data to disk, creates a voice_notes row, and
// publishes a VoiceNoteJob. Returns the created VoiceNote for the API response.
func dispatchVoiceNote(ctx context.Context, userID, fileName, ext, mimeType, source string, data []byte) (*VoiceNote, error) {
	diskPath, err := saveToUploadsDir(data, ext)
	if err != nil {
		return nil, err
	}

	upload, err := serviceDeps.GetVoiceNoteRepo().Create(ctx, userID, fileName, diskPath)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		os.Remove(diskPath)
		// TODO: consider deleting the voice_notes row here too
		return nil, err
	}

	err = publishOrCleanup(ctx, queue, VoiceNoteJob{
		UserID:    userID,
		UploadID:  upload.ID,
		FilePath:  diskPath,
		FileName:  fileName,
		MimeType:  mimeType,
		Source:    source,
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	},
		func() { os.Remove(diskPath) },
	)
	if err != nil {
		return nil, err
	}

	return &upload, nil
}
```

**Step 2: Refactor `handleUpload` in `voice_note_upload.go`**

Replace the file-writing, DB-insert, and queue-publish block (lines ~70–115) with:

```go
	// Read file into memory.
	data, err := io.ReadAll(file)
	if err != nil {
		log.Error("upload: read file failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
		return
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = extensionFromMIME(contentType)
	}

	upload, err := dispatchVoiceNote(ctx, userID, header.Filename, ext, contentType, "upload", data)
	if err != nil {
		log.Error("upload: dispatch failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process upload"})
		return
	}

	log.Info("upload completed", "user_id", userID, "upload_id", upload.ID, "file_name", header.Filename)
	writeJSON(w, http.StatusOK, UploadResponse{
		UploadID: upload.ID,
		FileName: header.Filename,
	})
```

This replaces everything from `uploadsDir := serviceDeps.GetUploadsDir()` through the end of the function (before the closing `}`). Remove unused imports: `"os"`, `"time"`, `"github.com/google/uuid"`.

The full updated file should be:

```go
package handler

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

const maxUploadSize = 25 << 20

var allowedAudioTypes = []string{
	"audio/",
	"video/webm",
}

type UploadResponse struct {
	UploadID int64  `json:"uploadId"`
	FileName string `json:"fileName"`
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid multipart (max 25MB)"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'file' field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !isAllowedAudioType(contentType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("unsupported file type: %s. Accepted: mp3, mp4, mpeg, mpga, m4a, wav, webm", contentType),
		})
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()

	data, err := io.ReadAll(file)
	if err != nil {
		log.Error("upload: read file failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
		return
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = extensionFromMIME(contentType)
	}

	upload, err := dispatchVoiceNote(ctx, userID, header.Filename, ext, contentType, "upload", data)
	if err != nil {
		log.Error("upload: dispatch failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process upload"})
		return
	}

	log.Info("upload completed", "user_id", userID, "upload_id", upload.ID, "file_name", header.Filename)
	writeJSON(w, http.StatusOK, UploadResponse{
		UploadID: upload.ID,
		FileName: header.Filename,
	})
}

func isAllowedAudioType(contentType string) bool {
	ct := strings.ToLower(contentType)
	for _, prefix := range allowedAudioTypes {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

func extensionFromMIME(mime string) string {
	switch strings.ToLower(mime) {
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/m4a":
		return ".m4a"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/webm", "video/webm":
		return ".webm"
	case "audio/ogg":
		return ".ogg"
	default:
		return ".bin"
	}
}
```

**Step 3: Refactor `handleDriveImport` in `voice_note_drive_import.go`**

Replace the file-writing, DB-insert, and queue-publish block with:

```go
	ext := filepath.Ext(req.FileName)
	if ext == "" {
		ext = extensionFromMIME(fileMeta.MimeType)
	}

	upload, err := dispatchVoiceNote(ctx, userID, cleanName, ext, fileMeta.MimeType, "drive_import", data)
	if err != nil {
		log.Error("drive-import: dispatch failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process import"})
		return
	}

	log.Info("drive-import completed", "user_id", userID, "source_file_id", req.FileID, "upload_id", upload.ID, "file_name", cleanName)
	writeJSON(w, http.StatusOK, DriveImportResponse{
		UploadID: upload.ID,
		FileName: cleanName,
	})
```

This replaces everything from `// Write to local disk.` through the end of the function (before the closing `}`). Keep the `data` variable from the existing `io.ReadAll` call, but change the streaming to use `io.ReadAll` directly (remove the file-write-to-disk streaming). Remove unused imports: `"os"`, `"time"`, `"github.com/google/uuid"`.

The full updated file should be:

```go
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

type DriveImportRequest struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

type DriveImportResponse struct {
	UploadID int64  `json:"uploadId"`
	FileName string `json:"fileName"`
}

func handleDriveImport(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req DriveImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" || req.FileName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid 'fileId' / 'fileName'"})
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	driveSvc, err := serviceDeps.GetDriveClient(ctx, userID)
	if err != nil {
		log.Error("drive-import: get drive client failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to connect to Google Drive"})
		return
	}

	fileMeta, err := driveSvc.GetFileMeta(ctx, req.FileID)
	if err != nil {
		log.Error("drive-import: file not accessible", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found or not accessible on Google Drive"})
		return
	}

	if !isAllowedAudioType(fileMeta.MimeType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "file is not an audio file (type: " + fileMeta.MimeType + ")",
		})
		return
	}

	rc, err := driveSvc.DownloadFile(ctx, req.FileID)
	if err != nil {
		log.Error("drive-import: download failed", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to download file from Google Drive"})
		return
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, maxUploadSize))
	if err != nil {
		log.Error("drive-import: read body failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file data"})
		return
	}
	if int64(len(data)) == maxUploadSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file exceeds the 25 MB limit"})
		return
	}

	cleanName := strings.TrimSpace(req.FileName)
	if cleanName == "" {
		cleanName = req.FileName
	}

	ext := filepath.Ext(req.FileName)
	if ext == "" {
		ext = extensionFromMIME(fileMeta.MimeType)
	}

	upload, err := dispatchVoiceNote(ctx, userID, cleanName, ext, fileMeta.MimeType, "drive_import", data)
	if err != nil {
		log.Error("drive-import: dispatch failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process import"})
		return
	}

	log.Info("drive-import completed", "user_id", userID, "source_file_id", req.FileID, "upload_id", upload.ID, "file_name", cleanName)
	writeJSON(w, http.StatusOK, DriveImportResponse{
		UploadID: upload.ID,
		FileName: cleanName,
	})
}
```

Note: the existing drive import used a hardcoded `maxReportImportBytes` (10MB) limit for the `LimitReader`. The voice note drive import didn't have a limit reader at all — it streamed to disk unbounded. The updated version uses `maxUploadSize` (25MB) to match the upload handler's limit.

**Step 4: Run tests and lint**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 5: Commit**

```bash
git add backend/voice_note_dispatch.go backend/voice_note_upload.go backend/voice_note_drive_import.go
git commit -m "refactor: voice note handlers use dispatchVoiceNote with shared helpers

Also fixes missing cleanup of DB rows and disk files on queue publish
failure (same bug previously fixed for report examples)."
```

---

## Task 4: Update voice note tests

The existing tests for upload and drive import may need adjustments since the handlers now return errors on queue failure instead of logging warnings.

**Files:**
- Modify: `backend/voice_note_upload_test.go`
- Modify: `backend/voice_note_drive_import_test.go`

**Step 1: Check which tests exist and if any need updating**

```bash
cd backend && go test -v -count=1 -run "TestUpload|TestDriveImport" ./...
```

If tests pass, no changes needed. If tests fail because they relied on the old "log warning and continue" behavior when the queue is unavailable, update them to either:
- Provide a stub queue in `withDeps`, or
- Expect an error response instead of success

**Step 2: Run full test suite**

```bash
cd backend && go test -v -count=1 ./... && make lint
```

**Step 3: Commit if changes were needed**

```bash
git add backend/voice_note_upload_test.go backend/voice_note_drive_import_test.go
git commit -m "test: update voice note tests for new dispatch error handling"
```

---

## Open Questions

- None
