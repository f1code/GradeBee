// students.go handles the GET /students endpoint that reads the user's
// ClassSetup spreadsheet from Google Sheets and returns the student roster
// grouped by class in alphabetical order. It also exposes parseStudentRows,
// a pure helper used directly by unit tests.
package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type studentsResponse struct {
	SpreadsheetURL string       `json:"spreadsheetUrl"`
	Classes        []classGroup `json:"classes"`
}

type classGroup struct {
	Name     string    `json:"name"`
	Students []student `json:"students"`
}

type student struct {
	Name string `json:"name"`
}

func handleGetStudents(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("get students failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	roster, err := serviceDeps.GetRoster(ctx, svc)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("get students failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	classes, err := roster.Students(ctx)
	if err != nil {
		log.Warn("get students failed", "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error":          "empty_spreadsheet",
			"message":        err.Error(),
			"spreadsheetUrl": roster.SpreadsheetURL(),
		})
		return
	}

	classCount := len(classes)
	studentCount := 0
	for _, c := range classes {
		studentCount += len(c.Students)
	}
	log.Info("get students completed", "user_id", svc.User.UserID, "class_count", classCount, "student_count", studentCount)

	writeJSON(w, http.StatusOK, studentsResponse{
		SpreadsheetURL: roster.SpreadsheetURL(),
		Classes:        classes,
	})
}

// parseStudentRows takes raw spreadsheet values ([][]interface{}) and returns
// grouped classes. First row is assumed to be a header and is skipped.
func parseStudentRows(rows [][]interface{}) ([]classGroup, error) {
	if len(rows) <= 1 {
		return nil, fmt.Errorf("No students found. Add your students to the ClassSetup spreadsheet.")
	}

	classMap := make(map[string][]student)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 2 {
			continue
		}
		classVal := strings.TrimSpace(fmt.Sprintf("%v", row[0]))
		studentVal := strings.TrimSpace(fmt.Sprintf("%v", row[1]))
		if classVal == "" || studentVal == "" {
			continue
		}
		classMap[classVal] = append(classMap[classVal], student{Name: studentVal})
	}

	if len(classMap) == 0 {
		return nil, fmt.Errorf("No students found. Add your students to the ClassSetup spreadsheet.")
	}

	// Sort classes alphabetically, sort students within each class
	var classNames []string
	for name := range classMap {
		classNames = append(classNames, name)
	}
	sort.Strings(classNames)

	var result []classGroup
	for _, name := range classNames {
		students := classMap[name]
		sort.Slice(students, func(i, j int) bool { return students[i].Name < students[j].Name })
		result = append(result, classGroup{Name: name, Students: students})
	}
	return result, nil
}
