// drive_import_example.go handles the POST /drive-import-example endpoint that
// downloads a Google Drive file, extracts text if needed, and stores it as a
// report example.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// allowedReportMIMETypes lists the MIME types accepted for report card import.
var allowedReportMIMETypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/webp":      true,
	"text/plain":      true,
	"text/markdown":   true,
}

const maxReportImportBytes = 10 << 20 // 10 MB

func handleDriveImportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req driveImportRequest
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
		log.Error("drive-import-example: get drive client failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to connect to Google Drive"})
		return
	}

	// Validate file metadata.
	fileMeta, err := driveSvc.Files.Get(req.FileID).Fields("mimeType").Context(ctx).Do()
	if err != nil {
		log.Error("drive-import-example: file not accessible", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found or not accessible on Google Drive"})
		return
	}

	if !allowedReportMIMETypes[fileMeta.MimeType] {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unsupported file type: " + fileMeta.MimeType + ". Allowed: PDF, PNG, JPEG, WebP, plain text, markdown",
		})
		return
	}

	// Download file from Drive (capped at maxReportImportBytes).
	resp, err := driveSvc.Files.Get(req.FileID).Context(ctx).Download()
	if err != nil {
		log.Error("drive-import-example: download failed", "file_id", req.FileID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to download file from Google Drive"})
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReportImportBytes))
	if err != nil {
		log.Error("drive-import-example: read body failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file data"})
		return
	}
	if len(data) == maxReportImportBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file exceeds the 10 MB limit"})
		return
	}

	name := strings.TrimSpace(req.FileName)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fileName must not be blank"})
		return
	}

	// Extract or decode content based on MIME type (more reliable than extension).
	isTextMIME := fileMeta.MimeType == "text/plain" || fileMeta.MimeType == "text/markdown"
	var content string
	if !isTextMIME {
		extractor, err := serviceDeps.GetExampleExtractor()
		if err != nil {
			log.Error("drive-import-example: get extractor failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "text extraction unavailable"})
			return
		}
		content, err = extractor.ExtractText(ctx, name, data)
		if err != nil {
			log.Error("drive-import-example: extraction failed", "error", err, "filename", name)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to extract text from file"})
			return
		}
	} else {
		// Text file — use content directly.
		content = string(data)
	}

	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no text content could be extracted from the file"})
		return
	}

	// Store as report example.
	store := serviceDeps.GetExampleStore()
	example, err := store.UploadExample(ctx, userID, name, content)
	if err != nil {
		log.Error("drive-import-example: store failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	log.Info("drive-import-example completed", "user_id", userID, "source_file_id", req.FileID, "example_id", example.ID, "file_name", name)
	writeJSON(w, http.StatusOK, example)
}
