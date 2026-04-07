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