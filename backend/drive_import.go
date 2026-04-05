// drive_import.go handles the POST /drive-import endpoint that downloads a
// Google Drive file to local disk, creates an uploads row, and dispatches
// an async processing job.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DriveImportRequest is the JSON body for POST /drive-import.
type DriveImportRequest struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

// DriveImportResponse is the JSON response for POST /drive-import.
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

	// Get Drive read client.
	driveSvc, err := serviceDeps.GetDriveClient(ctx, userID)
	if err != nil {
		log.Error("drive-import: get drive client failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to connect to Google Drive"})
		return
	}

	// Validate file is accessible and is an audio file.
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

	// Download file from Drive.
	rc, err := driveSvc.DownloadFile(ctx, req.FileID)
	if err != nil {
		log.Error("drive-import: download failed", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to download file from Google Drive"})
		return
	}
	defer rc.Close()

	// Write to local disk.
	uploadsDir := serviceDeps.GetUploadsDir()
	ext := filepath.Ext(req.FileName)
	if ext == "" {
		ext = extensionFromMIME(fileMeta.MimeType)
	}
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)

	dst, err := os.Create(diskPath)
	if err != nil {
		log.Error("drive-import: create file failed", "error", err, "path", diskPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file"})
		return
	}
	if _, err := io.Copy(dst, rc); err != nil {
		dst.Close()
		os.Remove(diskPath)
		log.Error("drive-import: write file failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file"})
		return
	}
	dst.Close()

	// Insert uploads row.
	cleanName := strings.TrimSpace(req.FileName)
	if cleanName == "" {
		cleanName = req.FileName
	}

	upload, err := serviceDeps.GetVoiceNoteRepo().Create(ctx, userID, cleanName, diskPath)
	if err != nil {
		os.Remove(diskPath)
		log.Error("drive-import: insert row failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record upload"})
		return
	}

	log.Info("drive-import completed", "user_id", userID, "source_file_id", req.FileID, "upload_id", upload.ID, "file_name", cleanName)

	// Dispatch async processing job.
	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		log.Warn("drive-import: queue unavailable, skipping async processing", "error", err)
	} else {
		if err := queue.Publish(ctx, VoiceNoteJob{
			UserID:    userID,
			UploadID:  upload.ID,
			FilePath:  diskPath,
			FileName:  cleanName,
			MimeType:  fileMeta.MimeType,
			Source:    "drive_import",
			Status:    JobStatusQueued,
			CreatedAt: time.Now(),
		}); err != nil {
			log.Error("drive-import: failed to dispatch job", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, DriveImportResponse{
		UploadID: upload.ID,
		FileName: cleanName,
	})
}
