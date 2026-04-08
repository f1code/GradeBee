// voice_note_drive_import.go handles POST /voice-notes/drive-import — downloads a Google Drive file, saves it to local disk, and dispatches an async processing job.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// DriveImportRequest is the JSON request body for POST /voice-notes/drive-import.
type DriveImportRequest struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

// DriveImportResponse is the JSON response for POST /voice-notes/drive-import.
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
