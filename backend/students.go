package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"google.golang.org/api/drive/v3"
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
	rootID, err := getGradeBeeRootID(ctx, svc.Drive)
	if err != nil {
		writeAPIError(w, r, &apiError{
			Status:  http.StatusNotFound,
			Code:    "no_spreadsheet",
			Message: "ClassSetup spreadsheet not found. Try running setup again.",
		})
		return
	}

	spreadsheetID, err := findClassSetupSpreadsheet(ctx, svc.Drive, rootID)
	if err != nil {
		writeAPIError(w, r, &apiError{
			Status:  http.StatusNotFound,
			Code:    "no_spreadsheet",
			Message: "ClassSetup spreadsheet not found. Try running setup again.",
		})
		return
	}

	// Read spreadsheet data
	readRange := "Students!A:B"
	resp, err := svc.Sheets.Spreadsheets.Values.Get(spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		log.Error("get students failed", "error", err, "spreadsheet_id", spreadsheetID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Convert [][]interface{} to [][]interface{} for parseStudentRows
	var rows [][]interface{}
	if resp.Values != nil {
		rows = resp.Values
	}

	classes, err := parseStudentRows(rows)
	if err != nil {
		log.Warn("get students failed", "error", err, "spreadsheet_id", spreadsheetID)
		spreadsheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", spreadsheetID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error":          "empty_spreadsheet",
			"message":        err.Error(),
			"spreadsheetUrl": spreadsheetURL,
		})
		return
	}

	classCount := len(classes)
	studentCount := 0
	for _, c := range classes {
		studentCount += len(c.Students)
	}
	log.Info("get students completed", "user_id", svc.User.UserID, "class_count", classCount, "student_count", studentCount)

	spreadsheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", spreadsheetID)
	writeJSON(w, http.StatusOK, studentsResponse{
		SpreadsheetURL: spreadsheetURL,
		Classes:        classes,
	})
}

func findClassSetupSpreadsheet(ctx context.Context, srv *drive.Service, rootID string) (string, error) {
	q := fmt.Sprintf("name='ClassSetup' and '%s' in parents and mimeType='application/vnd.google-apps.spreadsheet' and trashed=false", rootID)
	result, err := srv.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(result.Files) == 0 {
		return "", fmt.Errorf("ClassSetup not found")
	}
	return result.Files[0].Id, nil
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
