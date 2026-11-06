// terms.go handles Admin writes to a Group's Terms from the Levels screen.
// Reads ride on GET /api/levels.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// termRequest is the body of POST /api/terms and PUT /api/terms/{id}.
type termRequest struct {
	Name      string `json:"name"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// decodeTerm writes a 400 and returns ok=false when the body is not a valid Term.
func decodeTerm(w http.ResponseWriter, r *http.Request) (req termRequest, ok bool) {
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return req, false
	}
	req.Name = strings.TrimSpace(req.Name)
	msg := ""
	switch {
	case req.Name == "":
		msg = "name is required"
	case !isDate(req.StartDate) || !isDate(req.EndDate):
		// Dates compare as TEXT in SQL, so only YYYY-MM-DD may land.
		msg = "dates must be YYYY-MM-DD"
	case req.StartDate > req.EndDate:
		msg = "start date is after end date"
	}
	if msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return req, false
	}
	return req, true
}

func isDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// requireAdminGroup is requireGroup plus the Admin gate.
func requireAdminGroup(w http.ResponseWriter, r *http.Request) (groupID string, ok bool) {
	groupID, ok = requireGroup(w, r)
	if ok && !isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return "", false
	}
	return groupID, ok
}

// writeTermError maps TermRepo write errors to responses.
func writeTermError(w http.ResponseWriter, r *http.Request, err error) {
	var overlap *ErrTermOverlap
	switch {
	case errors.As(err, &overlap):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Overlaps " + overlap.Name})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "term not found"})
	case errors.Is(err, ErrTermInUse):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Term in use"})
	default:
		writeInternalError(w, r, err)
	}
}

func handleCreateTerm(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireAdminGroup(w, r)
	if !ok {
		return
	}
	req, ok := decodeTerm(w, r)
	if !ok {
		return
	}
	t, err := serviceDeps.GetLevelRepo().Terms().Create(r.Context(), groupID, req.Name, req.StartDate, req.EndDate)
	if err != nil {
		writeTermError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func handleUpdateTerm(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireAdminGroup(w, r)
	if !ok {
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid term id"})
		return
	}
	req, ok := decodeTerm(w, r)
	if !ok {
		return
	}
	t, err := serviceDeps.GetLevelRepo().Terms().Update(r.Context(), groupID, id, req.Name, req.StartDate, req.EndDate)
	if err != nil {
		writeTermError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func handleDeleteTerm(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireAdminGroup(w, r)
	if !ok {
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid term id"})
		return
	}
	if err := serviceDeps.GetLevelRepo().Terms().Delete(r.Context(), groupID, id); err != nil {
		writeTermError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
