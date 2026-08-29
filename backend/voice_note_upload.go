// voice_note_upload.go handles POST /voice-notes/upload — receives an audio file via multipart/form-data, saves it to local disk, and dispatches an async processing job.
package handler

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
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

	contentType := header.Header.Get("Content-Type")
	// Validate MIME type.
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

	ext := audioExtension(header.Filename, contentType)

	upload, err := dispatchVoiceNote(ctx, userID, header.Filename, ext, contentType, "upload", data)
	if err != nil {
		log.Error("upload: dispatch failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process upload"})
		return
	}

	log.Info("upload completed", "user_id", userID, "upload_id", upload.ID, "file_ext", ext)
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

// audioExtension returns the extension to use for a recording, given the name
// the teacher's file arrived under and its declared MIME type.
//
// It is deliberately not `filepath.Ext`, which returns everything after the
// final dot and so is not a validated extension: `Dr. Manoe 12 sept` has no
// extension at all, but `filepath.Ext` reads one of ". Manoe 12 sept". That
// matters twice over, because the result is both logged (ADR 0003: no child
// name reaches telemetry) and concatenated into the on-disk name by
// `saveToUploadsDir`, whose path is logged on the cleanup and transcription
// paths — and, via `*PathError`, inside error strings too.
//
// The test is **membership, not shape**. Checking that the trailing segment
// merely looks like an extension is not enough: `M.` / `Dr.` prefixes are
// ordinary on this product, and written without a space (`Dr.Manoe`) the given
// name itself passes any plausible shape rule — most given names we see are
// short and alphabetic. Only an extension we actually accept is trusted;
// anything else falls back to the MIME type, which the handler has already
// validated. Falling back more often is free, because nothing downstream reads
// the on-disk extension — transcription is handed the teacher's original name.
func audioExtension(fileName, mimeType string) string {
	if ext := strings.ToLower(filepath.Ext(fileName)); allowedAudioExts[ext] {
		return ext
	}
	return extensionFromMIME(mimeType)
}

// allowedAudioExts is the set of extensions a recording may be stored and
// logged under: exactly the outputs of extensionFromMIME, minus its .bin
// fallback. Keep the two in step. It is deliberately *not* tied to the
// caller-facing list in the unsupported-type error above, which is shorter —
// that list describes what a teacher may upload, and narrowing this set to
// match it would push accepted formats onto the .bin fallback for no gain.
var allowedAudioExts = map[string]bool{
	".mp3":  true,
	".mp4":  true,
	".mpeg": true,
	".mpga": true,
	".m4a":  true,
	".wav":  true,
	".webm": true,
	".ogg":  true,
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
