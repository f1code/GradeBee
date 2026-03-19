// notes_handler.go handles the POST /notes endpoint that creates Google Doc
// notes for confirmed student observations.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
)

type createNotesRequest struct {
	FileID     string              `json:"fileId"`
	Students   []noteStudentInput  `json:"students"`
	Transcript string              `json:"transcript"`
	Date       string              `json:"date"`
}

type noteStudentInput struct {
	Name    string `json:"name"`
	Class   string `json:"class"`
	Summary string `json:"summary"`
}

type createNotesResponse struct {
	Notes []noteResult `json:"notes"`
}

type noteResult struct {
	Student string `json:"student"`
	Class   string `json:"class"`
	DocID   string `json:"docId"`
	DocURL  string `json:"docUrl"`
}

func handleCreateNotes(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	var req createNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(req.Students) == 0 || req.Transcript == "" || req.Date == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required fields (students, transcript, date)"})
		return
	}

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("create notes failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Get notes folder ID from metadata.
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil || meta == nil || meta.NotesID == "" {
		log.Error("create notes: notes folder not found", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notes folder not configured, run setup first"})
		return
	}

	noteCreator := serviceDeps.GetNoteCreator(svc)

	var notes []noteResult
	for _, s := range req.Students {
		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			NotesRootID: meta.NotesID,
			StudentName: s.Name,
			ClassName:   s.Class,
			Summary:     s.Summary,
			Transcript:  req.Transcript,
			Date:        req.Date,
		})
		if err != nil {
			log.Error("create notes: failed for student", "student", s.Name, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create note for " + s.Name})
			return
		}
		notes = append(notes, noteResult{
			Student: s.Name,
			Class:   s.Class,
			DocID:   result.DocID,
			DocURL:  result.DocURL,
		})
	}

	log.Info("create notes completed", "user_id", svc.User.UserID, "note_count", len(notes))
	writeJSON(w, http.StatusOK, createNotesResponse{Notes: notes})
}
