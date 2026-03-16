// setup.go handles the POST /setup endpoint that provisions a user's GradeBee
// workspace in Google Drive. On first call it creates a "GradeBee" root folder,
// subfolders (uploads, notes, reports), and a pre-populated "ClassSetup"
// spreadsheet, then persists the resulting IDs in Clerk metadata. Subsequent
// calls are idempotent: existing resources are reused and only missing pieces
// are recreated.
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
	userID := svc.User.UserID

	// Check metadata for existing IDs (avoids Drive Files.List which requires restricted scope)
	meta, err := getGradeBeeMetadata(ctx, userID)
	if err != nil {
		log.Warn("setup: could not read metadata", "error", err)
	}
	if meta != nil && meta.FolderID != "" {
		// Verify folder still exists (drive.file allows Get on files we created)
		_, err := svc.Drive.Files.Get(meta.FolderID).Fields("id").Context(ctx).Do()
		if err == nil {
			// Folder exists. Check spreadsheet.
			if meta.SpreadsheetID != "" {
				_, err := svc.Drive.Files.Get(meta.SpreadsheetID).Fields("id").Context(ctx).Do()
				if err == nil {
					// Both exist. Ensure subfolders if missing.
					rootID, spreadsheetID, spreadsheetURL, err := ensureSubfolders(ctx, svc, meta)
					if err != nil {
						log.Error("setup failed", "step", "ensureSubfolders", "error", err)
						writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
						return
					}
					log.Info("setup completed (existing)", "user_id", userID, "folder_id", rootID, "spreadsheet_id", spreadsheetID)
					writeJSON(w, http.StatusOK, setupResponse{
						FolderID:       rootID,
						FolderURL:      fmt.Sprintf("https://drive.google.com/drive/folders/%s", rootID),
						SpreadsheetID:  spreadsheetID,
						SpreadsheetURL: spreadsheetURL,
					})
					return
				}
			}
			// Folder exists but spreadsheet missing. Create spreadsheet and store.
			spreadsheetID, spreadsheetURL, err := createAndMoveClassSetup(ctx, svc, meta.FolderID)
			if err != nil {
				log.Error("setup failed", "step", "createClassSetup", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			meta.SpreadsheetID = spreadsheetID
			if err := setGradeBeeMetadata(ctx, userID, meta); err != nil {
				log.Warn("setup: could not store spreadsheet_id in metadata", "error", err)
			}
			log.Info("setup completed (recreated spreadsheet)", "user_id", userID, "folder_id", meta.FolderID, "spreadsheet_id", spreadsheetID)
			writeJSON(w, http.StatusOK, setupResponse{
				FolderID:       meta.FolderID,
				FolderURL:      fmt.Sprintf("https://drive.google.com/drive/folders/%s", meta.FolderID),
				SpreadsheetID:  spreadsheetID,
				SpreadsheetURL: spreadsheetURL,
			})
			return
		}
		// Folder was deleted. Fall through to create fresh.
	}

	// Create fresh: folder, subfolders, spreadsheet
	rootID, err := createFolder(svc.Drive, "root", "GradeBee")
	if err != nil {
		log.Error("setup failed", "step", "createFolder", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	meta = &gradeBeeMetadata{FolderID: rootID}

	// Create subfolders
	subfolders := []struct {
		name string
		key  *string
	}{
		{"uploads", &meta.UploadsID},
		{"notes", &meta.NotesID},
		{"reports", &meta.ReportsID},
	}
	for _, sf := range subfolders {
		id, err := createFolder(svc.Drive, rootID, sf.name)
		if err != nil {
			log.Error("setup failed", "step", "createSubfolder", "name", sf.name, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		*sf.key = id
	}

	spreadsheetID, spreadsheetURL, err := createAndMoveClassSetup(ctx, svc, rootID)
	if err != nil {
		log.Error("setup failed", "step", "createClassSetup", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	meta.SpreadsheetID = spreadsheetID

	if err := setGradeBeeMetadata(ctx, userID, meta); err != nil {
		log.Error("setup failed", "step", "setGradeBeeMetadata", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	log.Info("setup completed", "user_id", userID, "folder_id", rootID, "spreadsheet_id", spreadsheetID)
	writeJSON(w, http.StatusOK, setupResponse{
		FolderID:       rootID,
		FolderURL:      fmt.Sprintf("https://drive.google.com/drive/folders/%s", rootID),
		SpreadsheetID:  spreadsheetID,
		SpreadsheetURL: spreadsheetURL,
	})
}

// ensureSubfolders creates subfolders if metadata is missing them. Returns rootID, spreadsheetID, spreadsheetURL.
func ensureSubfolders(ctx context.Context, svc *googleServices, meta *gradeBeeMetadata) (rootID, spreadsheetID, spreadsheetURL string, err error) {
	rootID = meta.FolderID
	spreadsheetID = meta.SpreadsheetID
	spreadsheetURL = fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", spreadsheetID)

	needsUpdate := meta.UploadsID == "" || meta.NotesID == "" || meta.ReportsID == ""
	if !needsUpdate {
		return rootID, spreadsheetID, spreadsheetURL, nil
	}

	subfolders := []struct {
		name string
		key  *string
	}{
		{"uploads", &meta.UploadsID},
		{"notes", &meta.NotesID},
		{"reports", &meta.ReportsID},
	}
	for _, sf := range subfolders {
		if *sf.key != "" {
			continue
		}
		id, createErr := createFolder(svc.Drive, rootID, sf.name)
		if createErr != nil {
			return "", "", "", fmt.Errorf("creating %s folder: %w", sf.name, createErr)
		}
		*sf.key = id
	}

	// Persist new subfolder IDs
	if err := setGradeBeeMetadata(ctx, svc.User.UserID, meta); err != nil {
		// Non-fatal: we have the IDs in memory
		loggerFromContext(ctx).Warn("ensureSubfolders: could not store metadata", "error", err)
	}
	return rootID, spreadsheetID, spreadsheetURL, nil
}

// createAndMoveClassSetup creates the ClassSetup spreadsheet and moves it into the GradeBee folder.
func createAndMoveClassSetup(ctx context.Context, svc *googleServices, rootID string) (spreadsheetID, spreadsheetURL string, err error) {
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

	_, err = svc.Drive.Files.Update(created.SpreadsheetId, nil).
		AddParents(rootID).
		RemoveParents("root").
		Context(ctx).
		Do()
	if err != nil {
		return "", "", fmt.Errorf("moving spreadsheet to GradeBee folder: %w", err)
	}

	return created.SpreadsheetId, fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", created.SpreadsheetId), nil
}

func strPtr(s string) *string { return &s }
