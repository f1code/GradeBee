// students.go handles CRUD handlers for classes and students.
// Student data is stored in SQLite.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ListClassesResponse is the JSON envelope for handleListClasses.
type ListClassesResponse struct {
	Classes []ClassWithCount `json:"classes"`
}

// ListStudentsResponse is the JSON envelope for handleListStudents.
type ListStudentsResponse struct {
	Students []Student `json:"students"`
}

// ClassGroup is one class as the extractor and the roster see it.
type ClassGroup struct {
	// ID is the classes row, so the done card can name the roster it offers.
	// The prompt builders read Name only, so it never reaches the model and
	// never moves ExtractionPromptHash; fixtures and sentinelClasses omit it.
	ID       int64          `json:"id,omitempty"`
	Name     string         `json:"name"`
	Students []ClassStudent `json:"students"`
}

type ClassStudent struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

func handleListClasses(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	classes, err := serviceDeps.GetClassRepo().List(r.Context(), userID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if classes == nil {
		classes = []ClassWithCount{}
	}
	writeJSON(w, http.StatusOK, ListClassesResponse{Classes: classes})
}

func handleCreateClass(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	groupID, ok := requireGroup(w, r)
	if !ok {
		return
	}
	var req struct {
		LevelID  int64  `json:"levelId"`
		Day      string `json:"day"`
		TimeSlot string `json:"timeSlot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LevelID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "levelId is required"})
		return
	}
	if !isValidDay(req.Day) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "day is required and must be a valid weekday"})
		return
	}
	c, err := serviceDeps.GetClassRepo().Create(r.Context(), groupID, userID, req.LevelID, req.Day, req.TimeSlot)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "level not found"})
			return
		}
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "class already exists"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ClassWithCount{Class: c, StudentCount: 0})
}

func handleUpdateClass(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	groupID, ok := requireGroup(w, r)
	if !ok {
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	var req struct {
		LevelID  int64  `json:"levelId"`
		Day      string `json:"day"`
		TimeSlot string `json:"timeSlot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LevelID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "levelId is required"})
		return
	}
	if !isValidDay(req.Day) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "day is required and must be a valid weekday"})
		return
	}
	if err := serviceDeps.GetClassRepo().Update(r.Context(), groupID, userID, id, req.LevelID, req.Day, req.TimeSlot); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "class or level not found"})
			return
		}
		if errors.Is(err, ErrDuplicate) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "class name already exists"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func handleDeleteClass(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	if err := serviceDeps.GetClassRepo().Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
			return
		}
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Student CRUD ---

func handleListStudents(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	classID, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid class id"})
		return
	}
	// Verify class ownership
	if err := verifyClassOwnership(r.Context(), classID, userID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
		return
	}
	students, err := serviceDeps.GetStudentRepo().ListWithAliases(r.Context(), classID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	if students == nil {
		students = []Student{}
	}
	writeJSON(w, http.StatusOK, ListStudentsResponse{Students: students})
}

func handleCreateStudent(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	classID, ok := idParam(r, "id")
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
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func handleUpdateStudent(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	if !requireStudentOwnership(w, r, id, userID, "student not found") {
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
			writeInternalError(w, r, err)
			return
		}
	}
	resp := map[string]interface{}{"status": "updated"}
	if req.ClassID != nil {
		if err := verifyClassOwnership(ctx, *req.ClassID, userID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "target class not found"})
			return
		}
		dropped, err := serviceDeps.GetStudentRepo().Move(ctx, id, *req.ClassID)
		if err != nil {
			var dupErr *ErrDuplicateStudentName
			if errors.As(err, &dupErr) {
				writeAPIError(w, r, &apiError{
					Status:  http.StatusConflict,
					Code:    "student_name_conflict",
					Message: fmt.Sprintf("A student named %q already exists in the target class.", dupErr.ConflictName),
					Details: map[string]string{"conflictStudentName": dupErr.ConflictName},
				})
				return
			}
			writeInternalError(w, r, err)
			return
		}
		if len(dropped) > 0 {
			resp["droppedAliases"] = dropped
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleDeleteStudent(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}
	if !requireStudentOwnership(w, r, id, userID, "student not found") {
		return
	}
	if err := serviceDeps.GetStudentRepo().Delete(r.Context(), id); err != nil {
		writeInternalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// verifyClassOwnership checks that a class belongs to the given user.
func verifyClassOwnership(ctx context.Context, classID int64, userID string) error {
	_, err := ownedClass(ctx, classID, userID)
	return err
}

// ownedClass returns the caller's class with this id, or ErrNotFound when it
// is somebody else's or nobody's. One scan over the caller's own list, so the
// ownership gate and the class it yields cannot disagree.
func ownedClass(ctx context.Context, classID int64, userID string) (ClassWithCount, error) {
	classes, err := serviceDeps.GetClassRepo().List(ctx, userID)
	if err != nil {
		return ClassWithCount{}, err
	}
	for _, c := range classes {
		if c.ID == classID {
			return c, nil
		}
	}
	return ClassWithCount{}, ErrNotFound
}
