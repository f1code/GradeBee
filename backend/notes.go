// notes.go implements the NoteCreator interface backed by SQLite and provides
// CRUD handlers for the /notes and /students/:id/notes endpoints.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// notes.source values. The column has no CHECK constraint, so these are the
// contract.
const (
	// NoteSourceAuto: written by the extraction pipeline end to end.
	NoteSourceAuto = "auto"
	// NoteSourceReviewed: the model wrote the text, the teacher supplied only
	// the who — a recording re-assembled against a class picked on the done
	// card.
	NoteSourceReviewed = "reviewed"
	// NoteSourceManual: typed by the teacher.
	NoteSourceManual = "manual"
	// NoteSourceAssigned: the teacher filed a passage that reached nobody to a
	// child from the done card. The text is the model's summary as the card
	// sent it back, so the server never saw the model produce it.
	NoteSourceAssigned = "assigned"
)

// isModelWritten says whether a note's text came from the model, which is what
// the implicit edit/delete thumbs-down measures. A reviewed note qualifies: the
// teacher named the child, the model wrote the sentence, and editing it away is
// the same signal about the same text. An assigned note does not: its text
// came over the wire from the card, so an edit to it says nothing the server
// can pin on extraction.
func isModelWritten(source string) bool {
	return source == NoteSourceAuto || source == NoteSourceReviewed
}

// NoteCreator creates notes in the database.
type NoteCreator interface {
	CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error)
}

// CreateNoteRequest is the input for creating a single student note.
type CreateNoteRequest struct {
	StudentID    int64
	StudentName  string
	QuotedText   string // Extracted passages from transcript
	Transcript   string
	Date         string // YYYY-MM-DD
	ModelVersion string // LLM model ID that produced this note (empty = NULL)
	// Source is a NoteSource* value; empty means NoteSourceAuto, so a caller
	// that does not care cannot write "" into the column.
	Source string
	// TraceID is the recording's key (voice_notes.trace_id). The pipeline,
	// assemble and assign set it; empty writes NULL, which is what a manual
	// note has.
	TraceID string
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
	promptHash := ExtractionPromptHash
	source := req.Source
	if source == "" {
		source = NoteSourceAuto
	}
	n := &Note{
		StudentID:  req.StudentID,
		Date:       req.Date,
		Summary:    req.QuotedText, // Store extracted passages as the note summary
		Source:     source,
		PromptHash: &promptHash,
	}
	if req.ModelVersion != "" {
		n.ModelVersion = &req.ModelVersion
	}
	if req.Transcript != "" {
		n.Transcript = &req.Transcript
	}
	if req.TraceID != "" {
		n.TraceID = &req.TraceID
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
		writeError(w, r, err)
		return
	}
	// Extract student ID from /students/{id}/notes
	studentID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
		return
	}
	notes, err := serviceDeps.GetNoteRepo().List(r.Context(), studentID)
	if err != nil {
		writeInternalError(w, r, err)
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
		writeError(w, r, err)
		return
	}
	studentID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
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
	// notes.date is a bare TEXT column compared with BETWEEN by the report query, so a
	// non-date string here silently excludes the note from every report instead of
	// erroring. The UI sends <input type="date">, but the API is reachable directly.
	if _, err := time.Parse(time.DateOnly, req.Date); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	n := &Note{
		StudentID: studentID,
		Date:      req.Date,
		Summary:   req.Summary,
		Source:    NoteSourceManual,
	}
	if err := serviceDeps.GetNoteRepo().Create(r.Context(), n); err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func handleGetNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	noteID, ok := idParam(r, "id")
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
		writeInternalError(w, r, err)
		return
	}
	if !requireStudentOwnership(w, r, n.StudentID, userID, "note not found") {
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	noteID, ok := idParam(r, "id")
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
		writeInternalError(w, r, err)
		return
	}
	if !requireStudentOwnership(w, r, n.StudentID, userID, "note not found") {
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
		writeInternalError(w, r, err)
		return
	}

	// Implicit signal: editing a model-written note records a thumbs-down with the
	// original summary. Only fire when the summary actually changed.
	if isModelWritten(n.Source) && n.Summary != req.Summary {
		if feedbackRepo := serviceDeps.GetFeedbackRepo(); feedbackRepo != nil {
			prev := n.Summary
			if _, fbErr := feedbackRepo.Insert(r.Context(), ArtifactFeedback{
				ArtifactType:  "note",
				ArtifactID:    noteID,
				Rating:        "down",
				Signal:        "edited",
				PreviousValue: &prev,
				UserID:        userID,
			}); fbErr != nil {
				loggerFromRequest(r).Warn("implicit edit feedback insert failed", "error", fbErr)
			}
		}
	}

	updated, err := serviceDeps.GetNoteRepo().GetByID(r.Context(), noteID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	noteID, ok := idParam(r, "id")
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
		writeInternalError(w, r, err)
		return
	}
	if !requireStudentOwnership(w, r, n.StudentID, userID, "note not found") {
		return
	}
	if err := serviceDeps.GetNoteRepo().Delete(r.Context(), noteID); err != nil {
		writeInternalError(w, r, err)
		return
	}

	// Implicit signal: deleting a model-written note records a thumbs-down with the
	// deleted summary. artifact_id will dangle (note row gone) — expected by design;
	// previous_value carries the content.
	if isModelWritten(n.Source) {
		if feedbackRepo := serviceDeps.GetFeedbackRepo(); feedbackRepo != nil {
			prev := n.Summary
			if _, fbErr := feedbackRepo.Insert(r.Context(), ArtifactFeedback{
				ArtifactType:  "note",
				ArtifactID:    noteID,
				Rating:        "down",
				Signal:        "deleted",
				PreviousValue: &prev,
				UserID:        userID,
			}); fbErr != nil {
				loggerFromRequest(r).Warn("implicit delete feedback insert failed", "error", fbErr)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
