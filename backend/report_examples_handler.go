// report_examples_handler.go handles the GET/POST/DELETE /report-examples
// endpoints for managing example report cards.
package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// ListExamplesResponse is the JSON envelope for handleListReportExamples.
type ListExamplesResponse struct {
	Examples []ReportExample `json:"examples"`
}

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

	writeJSON(w, http.StatusOK, ListExamplesResponse{Examples: examples})
}

func handleUploadReportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

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
		name := header.Filename
		if isExtractableFile(name) {
			// PDF or image — save to disk and dispatch async extraction.
			example, err := dispatchExtraction(r, userID, name, data)
			if err != nil {
				log.Error("failed to dispatch extraction", "error", err, "filename", name)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, example)
			return
		}
		// Plain text file — store directly.
		content := string(data)
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
		if req.Name == "" || req.Content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and content are required"})
			return
		}
		store := serviceDeps.GetExampleStore()
		example, err := store.UploadExample(r.Context(), userID, req.Name, req.Content)
		if err != nil {
			log.Error("upload report example failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, example)
	}
}

// dispatchExtraction saves a file to disk, creates a pending DB row, and
// publishes an ExtractionJob. Returns the pending example for the API response.
func dispatchExtraction(r *http.Request, userID, name string, data []byte) (*ReportExample, error) {
	uploadsDir := serviceDeps.GetUploadsDir()
	ext := filepath.Ext(name)
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)

	if err := os.WriteFile(diskPath, data, 0o644); err != nil {
		return nil, err
	}

	store := serviceDeps.GetExampleStore()
	example, err := store.CreatePendingExample(r.Context(), userID, name, diskPath)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	queue, err := serviceDeps.GetExtractionQueue()
	if err != nil {
		// Queue unavailable — clean up and return error.
		os.Remove(diskPath)
		return nil, err
	}
	if err := queue.Publish(r.Context(), ExtractionJob{
		UserID:    userID,
		ExampleID: example.ID,
		FilePath:  diskPath,
		FileName:  name,
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	}); err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	return example, nil
}

func handleUpdateReportExample(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	id, ok := pathParam(r.URL.Path, "/report-examples/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and content are required"})
		return
	}

	store := serviceDeps.GetExampleStore()
	example, err := store.UpdateExample(r.Context(), userID, id, req.Name, req.Content)
	if err != nil {
		log.Error("update report example failed", "error", err)
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
