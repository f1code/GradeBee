// extract_handler.go handles the POST /extract endpoint that analyzes a
// transcript and returns matched students for frontend confirmation.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
)

type extractRequest struct {
	Transcript string `json:"transcript"`
	FileID     string `json:"fileId"`
}

func handleExtract(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req extractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Transcript == "" || req.FileID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'transcript' or 'fileId'"})
		return
	}

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("extract failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Load roster for extraction context. Graceful degradation if unavailable.
	var classes []classGroup
	roster, err := serviceDeps.GetRoster(ctx, svc)
	if err != nil {
		log.Warn("extract: roster unavailable, proceeding without", "error", err)
	} else {
		classes, err = roster.Students(ctx)
		if err != nil {
			log.Warn("extract: could not read students", "error", err)
		}
	}

	extractor, err := serviceDeps.GetExtractor()
	if err != nil {
		log.Error("extract: extractor init failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "extraction service unavailable"})
		return
	}

	result, err := extractor.Extract(ctx, ExtractRequest{
		Transcript: req.Transcript,
		Classes:    classes,
	})
	if err != nil {
		log.Error("extract: extraction failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "extraction failed"})
		return
	}

	log.Info("extract completed", "user_id", svc.User.UserID, "student_count", len(result.Students))
	writeJSON(w, http.StatusOK, result)
}
