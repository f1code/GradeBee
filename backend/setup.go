package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"google.golang.org/api/sheets/v4"
)

type setupResponse struct {
	FolderID       string `json:"folderId"`
	FolderURL      string `json:"folderUrl"`
	SpreadsheetID  string `json:"spreadsheetId"`
	SpreadsheetURL string `json:"spreadsheetUrl"`
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	svc, err := serviceDeps.GoogleServices(r)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("setup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	rootID, err := ensureDriveFolders(ctx, svc)
	if err != nil {
		log.Error("setup failed", "step", "ensureDriveFolders", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	spreadsheetID, spreadsheetURL, err := ensureClassSetupSpreadsheet(ctx, svc, rootID)
	if err != nil {
		log.Error("setup failed", "step", "ensureClassSetupSpreadsheet", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	log.Info("setup completed", "user_id", svc.User.UserID, "folder_id", rootID, "spreadsheet_id", spreadsheetID)
	writeJSON(w, http.StatusOK, setupResponse{
		FolderID:       rootID,
		FolderURL:      fmt.Sprintf("https://drive.google.com/drive/folders/%s", rootID),
		SpreadsheetID:  spreadsheetID,
		SpreadsheetURL: spreadsheetURL,
	})
}

func ensureDriveFolders(ctx context.Context, svc *googleServices) (string, error) {
	// Find or create root GradeBee folder
	rootID, err := findOrCreateFolder(svc.Drive, "root", "GradeBee")
	if err != nil {
		return "", fmt.Errorf("creating root folder: %w", err)
	}

	// Create subfolders
	subfolders := []string{"uploads", "notes", "reports"}
	for _, name := range subfolders {
		if _, err := findOrCreateFolder(svc.Drive, rootID, name); err != nil {
			return "", fmt.Errorf("creating %s folder: %w", name, err)
		}
	}

	return rootID, nil
}

func ensureClassSetupSpreadsheet(ctx context.Context, svc *googleServices, rootID string) (spreadsheetID, spreadsheetURL string, err error) {
	// Check if ClassSetup already exists in GradeBee folder (idempotent)
	q := fmt.Sprintf("name='ClassSetup' and '%s' in parents and mimeType='application/vnd.google-apps.spreadsheet' and trashed=false", rootID)
	result, err := svc.Drive.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("searching for ClassSetup: %w", err)
	}
	if len(result.Files) > 0 {
		id := result.Files[0].Id
		return id, fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", id), nil
	}

	// Create new spreadsheet with Sheets API
	spreadsheet := &sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{Title: "ClassSetup"},
		Sheets: []*sheets.Sheet{{
			Properties: &sheets.SheetProperties{Title: "Students"},
			Data: []*sheets.GridData{{
				RowData: []*sheets.RowData{
					{
						Values: []*sheets.CellData{
							{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("class")}, UserEnteredFormat: &sheets.CellFormat{TextFormat: &sheets.TextFormat{Bold: true}}},
							{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("student")}, UserEnteredFormat: &sheets.CellFormat{TextFormat: &sheets.TextFormat{Bold: true}}},
						},
					},
					{Values: []*sheets.CellData{{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("5A")}}, {UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Emma Johnson")}}}},
					{Values: []*sheets.CellData{{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("5A")}}, {UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Liam Smith")}}}},
					{Values: []*sheets.CellData{{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("5B")}}, {UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Olivia Brown")}}}},
					{Values: []*sheets.CellData{{UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("5B")}}, {UserEnteredValue: &sheets.ExtendedValue{StringValue: strPtr("Noah Davis")}}}},
				},
			}},
		}},
	}

	created, err := svc.Sheets.Spreadsheets.Create(spreadsheet).Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("creating ClassSetup spreadsheet: %w", err)
	}

	// Move spreadsheet into GradeBee folder (created in user's root by default)
	_, err = svc.Drive.Files.Update(created.SpreadsheetId, nil).
		AddParents(rootID).
		RemoveParents("root").
		Context(ctx).
		Do()
	if err != nil {
		return "", "", fmt.Errorf("moving spreadsheet to GradeBee folder: %w", err)
	}

	url := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", created.SpreadsheetId)
	return created.SpreadsheetId, url, nil
}

func strPtr(s string) *string { return &s }
