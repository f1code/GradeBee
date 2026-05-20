// aliases.go implements HTTP handlers for student alias management.
// GET    /api/students/{id}/aliases             → list aliases
// POST   /api/students/{id}/aliases             → add alias
// DELETE /api/students/{id}/aliases/{aliasId}   → remove alias
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// AliasResponse is the JSON response for a single alias.
type AliasResponse struct {
	ID        int64  `json:"id"`
	StudentID int64  `json:"studentId"`
	Alias     string `json:"alias"`
	CreatedAt string `json:"createdAt"`
}

func handleListAliases(w http.ResponseWriter, r *http.Request) {
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

	aliases, err := serviceDeps.GetStudentRepo().ListAliases(r.Context(), studentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := make([]AliasResponse, len(aliases))
	for i, a := range aliases {
		result[i] = AliasResponse{ID: a.ID, StudentID: a.StudentID, Alias: a.Alias, CreatedAt: a.CreatedAt}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"aliases": result})
}

func handleAddAlias(w http.ResponseWriter, r *http.Request) {
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
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Alias) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alias is required"})
		return
	}

	a, err := serviceDeps.GetStudentRepo().AddAlias(r.Context(), studentID, strings.TrimSpace(req.Alias))
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "alias already in use in this class"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, AliasResponse{
		ID:        a.ID,
		StudentID: a.StudentID,
		Alias:     a.Alias,
		CreatedAt: a.CreatedAt,
	})
}

func handleRemoveAlias(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}

	// Path: /students/{studentId}/aliases/{aliasId}
	rest := strings.TrimPrefix(r.URL.Path, "/students/")
	parts := strings.SplitN(rest, "/aliases/", 2)
	if len(parts) != 2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	studentID, err2 := strconv.ParseInt(parts[0], 10, 64)
	aliasID, err3 := strconv.ParseInt(parts[1], 10, 64)
	if err2 != nil || err3 != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), studentID, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	}

	if err := serviceDeps.GetStudentRepo().RemoveAlias(r.Context(), studentID, aliasID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "alias not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
