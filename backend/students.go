// students.go handles the GET /students endpoint and CRUD handlers for
// classes and students. Student data is stored in SQLite.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Response types for the students API.
type studentsResponse struct {
	Classes []classGroupResponse `json:"classes"`
}

type classGroupResponse struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	StudentCount int               `json:"studentCount"`
	Students     []studentResponse `json:"students"`
}

type studentResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Internal types used by the extractor and roster (no IDs needed).
type classGroup struct {
	Name     string    `json:"name"`
	Students []student `json:"students"`
}

type student struct {
	Name string `json:"name"`
}

func handleGetStudents(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	classRepo := serviceDeps.GetClassRepo()
	studentRepo := serviceDeps.GetStudentRepo()

	classes, err := classRepo.List(ctx, userID)
	if err != nil {
		log.Error("get students failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := studentsResponse{Classes: make([]classGroupResponse, 0, len(classes))}
	for _, c := range classes {
		students, err := studentRepo.List(ctx, c.ID)
		if err != nil {
			log.Error("get students failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		cg := classGroupResponse{
			ID:           c.ID,
			Name:         c.Name,
			StudentCount: c.StudentCount,
			Students:     make([]studentResponse, len(students)),
		}
		for j, s := range students {
			cg.Students[j] = studentResponse{ID: s.ID, Name: s.Name}
		}
		resp.Classes = append(resp.Classes, cg)
	}

	log.Info("get students completed", "user_id", userID, "class_count", len(resp.Classes))
	writeJSON(w, http.StatusOK, resp)
}

// --- Class CRUD ---

func handleListClasses(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	classes, err := serviceDeps.GetClassRepo().List(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if classes == nil {
		classes = []ClassWithCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"classes": classes})
}

func handleCreateClass(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	c, err := serviceDeps.GetClassRepo().Create(r.Context(), userID, req.Name)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "class already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func handleUpdateClass(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	path := r.URL.Path
	id, ok := pathParam(path, "/classes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if err := serviceDeps.GetClassRepo().Rename(r.Context(), userID, id, req.Name); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
			return
		}
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "class name already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func handleDeleteClass(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	id, ok := pathParam(r.URL.Path, "/classes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	if err := serviceDeps.GetClassRepo().Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Student CRUD ---

func handleListStudents(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	classID, ok := pathParam(r.URL.Path, "/classes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	// Verify class ownership
	if err := verifyClassOwnership(r.Context(), classID, userID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
		return
	}
	students, err := serviceDeps.GetStudentRepo().List(r.Context(), classID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if students == nil {
		students = []Student{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"students": students})
}

func handleCreateStudent(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	classID, ok := pathParam(r.URL.Path, "/classes/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	if err := verifyClassOwnership(r.Context(), classID, userID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	s, err := serviceDeps.GetStudentRepo().Create(r.Context(), classID, req.Name)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "student already exists in this class"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func handleUpdateStudent(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	id, ok := pathParam(r.URL.Path, "/students/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), id, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	}
	var req struct {
		Name    string `json:"name"`
		ClassID *int64 `json:"classId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	ctx := r.Context()
	if req.Name != "" {
		if err := serviceDeps.GetStudentRepo().Rename(ctx, id, req.Name); err != nil {
			if errors.Is(err, ErrDuplicate) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "student name already exists in class"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.ClassID != nil {
		if err := verifyClassOwnership(ctx, *req.ClassID, userID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "target class not found"})
			return
		}
		if err := serviceDeps.GetStudentRepo().Move(ctx, id, *req.ClassID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func handleDeleteStudent(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	id, ok := pathParam(r.URL.Path, "/students/")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	owns, err := serviceDeps.GetStudentRepo().BelongsToUser(r.Context(), id, userID)
	if err != nil || !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	}
	if err := serviceDeps.GetStudentRepo().Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// verifyClassOwnership checks that a class belongs to the given user.
func verifyClassOwnership(ctx context.Context, classID int64, userID string) error {
	classes, err := serviceDeps.GetClassRepo().List(ctx, userID)
	if err != nil {
		return err
	}
	for _, c := range classes {
		if c.ID == classID {
			return nil
		}
	}
	return ErrNotFound
}
