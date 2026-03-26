// report_examples_handler.go handles the GET/POST/DELETE /report-examples
// endpoints for managing example report cards.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func handleListReportExamples(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("list report examples failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil || meta == nil || meta.ReportExamplesID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"examples": []any{}})
		return
	}

	store := serviceDeps.GetExampleStore(svc)
	examples, err := store.ListExamples(ctx, meta.ReportExamplesID)
	if err != nil {
		log.Error("list report examples failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"examples": examples})
}

// ensureReportExamplesFolder returns the report-examples folder ID, creating it on demand if needed.
func ensureReportExamplesFolder(ctx context.Context, svc *googleServices, meta *gradeBeeMetadata) (string, error) {
	if meta == nil || meta.FolderID == "" {
		return "", fmt.Errorf("GradeBee workspace not configured, run setup first")
	}
	if meta.ReportExamplesID != "" {
		return meta.ReportExamplesID, nil
	}
	id, err := createFolder(svc.Drive, meta.FolderID, "report-examples")
	if err != nil {
		return "", fmt.Errorf("creating report-examples folder: %w", err)
	}
	meta.ReportExamplesID = id
	if err := setGradeBeeMetadata(ctx, svc.User.UserID, meta); err != nil {
		loggerFromContext(ctx).Warn("ensureReportExamplesFolder: could not persist metadata", "error", err)
	}
	return id, nil
}

func handleUploadReportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("upload report example failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil {
		log.Error("upload report example: metadata lookup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read user metadata"})
		return
	}
	folderID, err := ensureReportExamplesFolder(ctx, svc, meta)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var name, content string

	contentType := r.Header.Get("Content-Type")
	if len(contentType) >= 19 && contentType[:19] == "multipart/form-data" {
		// Multipart upload
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read file"})
			return
		}
		name = header.Filename
		if isExtractableFile(name) {
			// PDF or image — extract text via GPT Vision
			extractor, err := serviceDeps.GetExampleExtractor()
			if err != nil {
				log.Error("failed to get example extractor", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "text extraction unavailable"})
				return
			}
			extracted, err := extractor.ExtractText(ctx, name, data)
			if err != nil {
				log.Error("failed to extract text from file", "error", err, "filename", name)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to extract text from file"})
				return
			}
			content = extracted
		} else {
			content = string(data)
		}
	} else {
		// JSON body with pasted text
		var req struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		name = req.Name
		content = req.Content
	}

	if name == "" || content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and content are required"})
		return
	}

	store := serviceDeps.GetExampleStore(svc)
	example, err := store.UploadExample(ctx, folderID, name, content)
	if err != nil {
		log.Error("upload report example failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, example)
}

func handleDeleteReportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("delete report example failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}

	store := serviceDeps.GetExampleStore(svc)
	if err := store.DeleteExample(r.Context(), req.ID); err != nil {
		log.Error("delete report example failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
