// levels.go handles CRUD handlers for the Levels admin screen. Levels are
// Group-owned: reads and writes are scoped to the caller's active Clerk
// Organization (see auth.go). Only an Admin may create, rename, delete a
// Level, or edit its Report Instructions — Teachers get a read-only list.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ListLevelsResponse is the JSON envelope for handleListLevels.
type ListLevelsResponse struct {
	Levels []Level `json:"levels"`
}

// requireGroup extracts the active Group ID from the request, writing the
// apiError response and returning ok=false on failure. groupIDFromRequest
// always returns *apiError on error, so callers write it directly rather
// than through a defensive fallback.
func requireGroup(w http.ResponseWriter, r *http.Request) (groupID string, ok bool) {
	groupID, err := groupIDFromRequest(r)
	if err != nil {
		var ae *apiError
		if !errors.As(err, &ae) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return "", false
		}
		writeAPIError(w, r, ae)
		return "", false
	}
	return groupID, true
}

func handleListLevels(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireGroup(w, r)
	if !ok {
		return
	}
	levels, err := serviceDeps.GetLevelRepo().List(r.Context(), groupID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if levels == nil {
		levels = []Level{}
	}
	writeJSON(w, http.StatusOK, ListLevelsResponse{Levels: levels})
}

func handleCreateLevel(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireGroup(w, r)
	if !ok {
		return
	}
	if !isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	name := ""
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		name = strings.TrimSpace(req.Name)
	}
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	l, err := serviceDeps.GetLevelRepo().Create(r.Context(), groupID, name)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a Level with this name already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func handleUpdateLevel(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireGroup(w, r)
	if !ok {
		return
	}
	if !isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	id, ok := pathParam(r.URL.Path, "/levels/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level id"})
		return
	}
	var req struct {
		Name               *string `json:"name"`
		ReportInstructions *string `json:"reportInstructions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == nil && req.ReportInstructions == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name or reportInstructions is required"})
		return
	}
	repo := serviceDeps.GetLevelRepo()
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name cannot be empty"})
			return
		}
		if err := repo.Rename(r.Context(), groupID, id, name); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "level not found"})
				return
			}
			if errors.Is(err, ErrDuplicate) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "a Level with this name already exists"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.ReportInstructions != nil {
		if err := repo.UpdateReportInstructions(r.Context(), groupID, id, *req.ReportInstructions); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "level not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	l, err := repo.GetByID(r.Context(), groupID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "level not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func handleDeleteLevel(w http.ResponseWriter, r *http.Request) {
	groupID, ok := requireGroup(w, r)
	if !ok {
		return
	}
	if !isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	id, ok := pathParam(r.URL.Path, "/levels/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level id"})
		return
	}
	if err := serviceDeps.GetLevelRepo().Delete(r.Context(), groupID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "level not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
