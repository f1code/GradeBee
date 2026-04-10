// notes.go implements the NoteCreator interface backed by SQLite and provides
// CRUD handlers for the /notes and /students/:id/notes endpoints.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// NoteCreator creates notes in the database.
type NoteCreator interface {
	CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error)
}

// CreateNoteRequest is the input for creating a single student note.
type CreateNoteRequest struct {
	StudentID   int64
	StudentName string
	QuotedText  string  // Extracted passages from transcript
	Transcript  string
	Date        string // YYYY-MM-DD
}

// CreateNoteResponse contains the created note info.
type CreateNoteResponse struct {
	NoteID int64 `json:"noteId"`
}

// dbNoteCreator creates notes in the SQLite database.
type dbNoteCreator struct {
	noteRepo *NoteRepo
}

func newDBNoteCreator(nr *NoteRepo) *dbNoteCreator {
	return &dbNoteCreator{noteRepo: nr}
}

func (c *dbNoteCreator) CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error) {
	n := &Note{
		StudentID: req.StudentID,
		Date:      req.Date,
		Summary:   req.QuotedText,  // Store extracted passages as the note summary
		Source:    "auto",
	}
	if req.Transcript != "" {
		n.Transcript = &req.Transcript
	}
	if err := c.noteRepo.Create(ctx, n); err != nil {
		return nil, err
	}
	return &CreateNoteResponse{NoteID: n.ID}, nil
}

// --- Note CRUD handlers ---

// ListNotesResponse is the JSON envelope for handleListNotes.
type ListNotesResponse struct {
	Notes []Note `json:"notes"`
}

func handleListNotes(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	// Extract student ID from /students/{id}/notes
	studentID, ok := pathParam(r.URL.Path, "/students/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), studentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	}
	notes, err := serviceDeps.GetNoteRepo().List(r.Context(), studentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if notes == nil {
		notes = []Note{}
	}
	writeJSON(w, http.StatusOK, ListNotesResponse{Notes: notes})
}

func handleCreateNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	studentID, ok := pathParam(r.URL.Path, "/students/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), studentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	}
	var req struct {
		Date    string `json:"date"`
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Summary == "" || req.Date == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date and summary are required"})
		return
	}
	n := &Note{
		StudentID: studentID,
		Date:      req.Date,
		Summary:   req.Summary,
		Source:    "manual",
	}
	if err := serviceDeps.GetNoteRepo().Create(r.Context(), n); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func handleGetNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	noteID, ok := pathParam(r.URL.Path, "/notes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note id"})
		return
	}
	n, err := serviceDeps.GetNoteRepo().GetByID(r.Context(), noteID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Verify ownership via student
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), n.StudentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	noteID, ok := pathParam(r.URL.Path, "/notes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note id"})
		return
	}
	n, err := serviceDeps.GetNoteRepo().GetByID(r.Context(), noteID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), n.StudentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
		return
	}
	var req struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Summary == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "summary is required"})
		return
	}
	if err := serviceDeps.GetNoteRepo().Update(r.Context(), noteID, req.Summary); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	updated, err := serviceDeps.GetNoteRepo().GetByID(r.Context(), noteID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	noteID, ok := pathParam(r.URL.Path, "/notes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid note id"})
		return
	}
	n, err := serviceDeps.GetNoteRepo().GetByID(r.Context(), noteID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), n.StudentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
		return
	}
	if err := serviceDeps.GetNoteRepo().Delete(r.Context(), noteID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
