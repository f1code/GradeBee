// report_examples_handler.go handles the GET/POST/DELETE /report-examples
// endpoints for managing example report cards.
package handler

import (
	"encoding/json"
	"io"
	"net/http"
)

func handleListReportExamples(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	store := serviceDeps.GetExampleStore()
	examples, err := store.ListExamples(r.Context(), userID)
	if err != nil {
		log.Error("list report examples failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if examples == nil {
		examples = []ReportExample{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"examples": examples})
}

func handleUploadReportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
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
			extracted, err := extractor.ExtractText(r.Context(), name, data)
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

	store := serviceDeps.GetExampleStore()
	example, err := store.UploadExample(r.Context(), userID, name, content)
	if err != nil {
		log.Error("upload report example failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, example)
}

func handleDeleteReportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}

	store := serviceDeps.GetExampleStore()
	if err := store.DeleteExample(r.Context(), userID, req.ID); err != nil {
		log.Error("delete report example failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
