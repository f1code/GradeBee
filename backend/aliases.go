// aliases.go implements HTTP handlers for student alias management.
// GET    /api/students/{id}/aliases             → list aliases
// POST   /api/students/{id}/aliases             → add alias
// DELETE /api/students/{id}/aliases/{aliasId}   → remove alias
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
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

	aliases, err := serviceDeps.GetStudentRepo().ListAliases(r.Context(), studentID)
	if err != nil {
		writeInternalError(w, r, err)
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
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Alias) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alias is required"})
		return
	}

	a, err := serviceDeps.GetStudentRepo().AddAlias(r.Context(), studentID, strings.TrimSpace(req.Alias))
	if err != nil {
		var dupErr *ErrDuplicateAlias
		if errors.As(err, &dupErr) {
			details := map[string]string{}
			if dupErr.ConflictStudentName != "" {
				details["conflictStudentName"] = dupErr.ConflictStudentName
			}
			writeAPIError(w, r, &apiError{
				Status:  http.StatusConflict,
				Code:    "alias_conflict",
				Message: "Alias already in use in this class.",
				Details: details,
			})
			return
		}
		writeInternalError(w, r, err)
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
		writeError(w, r, err)
		return
	}

	// Path: /api/students/{id}/aliases/{aliasID}
	studentID, ok1 := idParam(r, "id")
	aliasID, ok2 := idParam(r, "aliasID")
	if !ok1 || !ok2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
		return
	}

	if err := serviceDeps.GetStudentRepo().RemoveAlias(r.Context(), studentID, aliasID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "alias not found"})
			return
		}
		writeInternalError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
