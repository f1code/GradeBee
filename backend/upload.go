// upload.go handles the POST /upload endpoint that receives an audio file via
// multipart/form-data and stores it in the user's GradeBee/uploads/ folder on
// Google Drive.
package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/drive/v3"
)

const maxUploadSize = 25 << 20 // 25 MB (Whisper API limit)

// allowedAudioTypes lists MIME type prefixes accepted for upload.
var allowedAudioTypes = []string{
	"audio/",
	"video/webm",
}

type uploadResponse struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	// Enforce size limit before parsing.
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

	// Validate MIME type.
	contentType := header.Header.Get("Content-Type")
	if !isAllowedAudioType(contentType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("unsupported file type: %s. Accepted: mp3, mp4, mpeg, mpga, m4a, wav, webm", contentType),
		})
		return
	}

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("upload failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	userID := svc.User.UserID

	meta, err := getGradeBeeMetadata(ctx, userID)
	if err != nil || meta == nil || meta.UploadsID == "" {
		writeAPIError(w, r, &apiError{
			Status:  http.StatusNotFound,
			Code:    "no_uploads_folder",
			Message: "Uploads folder not found. Try running setup again.",
		})
		return
	}

	driveFile := &drive.File{
		Name:    header.Filename,
		Parents: []string{meta.UploadsID},
	}
	created, err := svc.Drive.Files.Create(driveFile).Media(file).Fields("id").Context(ctx).Do()
	if err != nil {
		log.Error("upload to Drive failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to upload to Google Drive"})
		return
	}

	log.Info("upload completed", "user_id", userID, "file_id", created.Id, "file_name", header.Filename)
	writeJSON(w, http.StatusOK, uploadResponse{
		FileID:   created.Id,
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
