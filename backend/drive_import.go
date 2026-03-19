// drive_import.go handles the POST /drive-import endpoint that validates a
// Google Drive file picked via Google Picker, copies it into the user's
// GradeBee/uploads/ folder, and returns the copy's file ID so the frontend
// can continue with the transcribe → extract → notes pipeline.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type driveImportRequest struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

type driveImportResponse struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

func handleDriveImport(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req driveImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" || req.FileName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid 'fileId' / 'fileName'"})
		return
	}

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("drive-import failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	userID := svc.User.UserID

	store := serviceDeps.GetDriveStore(svc)

	// Validate file is accessible and is an audio file.
	mimeType, err := store.GetMimeType(ctx, req.FileID)
	if err != nil {
		log.Error("drive-import: file not accessible", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found or not accessible on Google Drive"})
		return
	}

	if !isAllowedAudioType(mimeType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "file is not an audio file (type: " + mimeType + ")",
		})
		return
	}

	// Get uploads folder from Clerk metadata.
	meta, err := getGradeBeeMetadata(ctx, userID)
	if err != nil || meta == nil || meta.UploadsID == "" {
		writeAPIError(w, r, &apiError{
			Status:  http.StatusNotFound,
			Code:    "no_uploads_folder",
			Message: "Uploads folder not found. Try running setup again.",
		})
		return
	}

	// Copy file into GradeBee/uploads/.
	copyID, err := store.Copy(ctx, req.FileID, meta.UploadsID, req.FileName)
	if err != nil {
		log.Error("drive-import: copy failed", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to copy file to uploads folder"})
		return
	}

	// Trim any leading/trailing whitespace from filename for cleanliness.
	cleanName := strings.TrimSpace(req.FileName)
	if cleanName == "" {
		cleanName = req.FileName
	}

	log.Info("drive-import completed", "user_id", userID, "source_file_id", req.FileID, "copy_file_id", copyID, "file_name", cleanName)
	writeJSON(w, http.StatusOK, driveImportResponse{
		FileID:   copyID,
		FileName: cleanName,
	})
}
