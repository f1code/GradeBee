// voice_note_upload.go handles POST /voice-notes/upload — receives an audio file via multipart/form-data and saves it to local disk + the voice_notes table.
package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxUploadSize = 25 << 20 // 25 MB (Whisper API limit)

// allowedAudioTypes lists MIME type prefixes accepted for upload.
var allowedAudioTypes = []string{
	"audio/",
	"video/webm",
}

// UploadResponse is the JSON response for POST /upload.
type UploadResponse struct {
	UploadID int64  `json:"uploadId"`
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

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	ctx := r.Context()
	uploadsDir := serviceDeps.GetUploadsDir()

	// Generate unique filename and write to disk.
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = extensionFromMIME(contentType)
	}
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)

	dst, err := os.Create(diskPath)
	if err != nil {
		log.Error("upload: create file failed", "error", err, "path", diskPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file"})
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(diskPath)
		log.Error("upload: write file failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file"})
		return
	}
	dst.Close()

	// Insert uploads row.
	upload, err := serviceDeps.GetVoiceNoteRepo().Create(ctx, userID, header.Filename, diskPath)
	if err != nil {
		os.Remove(diskPath)
		log.Error("upload: insert row failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record upload"})
		return
	}

	log.Info("upload completed", "user_id", userID, "upload_id", upload.ID, "file_name", header.Filename)

	// Dispatch async processing job.
	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		log.Warn("upload: queue unavailable, skipping async processing", "error", err)
	} else {
		if err := queue.Publish(ctx, VoiceNoteJob{
			UserID:    userID,
			UploadID:  upload.ID,
			FilePath:  diskPath,
			FileName:  header.Filename,
			MimeType:  contentType,
			Source:    "upload",
			Status:    JobStatusQueued,
			CreatedAt: time.Now(),
		}); err != nil {
			log.Error("upload: failed to dispatch job", "error", err)
		}
	}

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

// extensionFromMIME returns a file extension for common audio MIME types.
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
