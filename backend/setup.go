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

	"google.golang.org/api/drive/v3"
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
		{"report-examples", &meta.ReportExamplesID},
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

	needsUpdate := meta.UploadsID == "" || meta.NotesID == "" || meta.ReportsID == "" || meta.ReportExamplesID == ""
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
		{"report-examples", &meta.ReportExamplesID},
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

// createAndMoveClassSetup creates the ClassSetup spreadsheet inside the
// GradeBee folder using the Drive API (so only drive.file scope is needed),
// then populates it via the Sheets API.
func createAndMoveClassSetup(ctx context.Context, svc *googleServices, rootID string) (spreadsheetID, spreadsheetURL string, err error) {
	// 1. Create an empty spreadsheet via Drive API directly in the target folder.
	driveFile := &drive.File{
		Name:     "ClassSetup",
		MimeType: "application/vnd.google-apps.spreadsheet",
		Parents:  []string{rootID},
	}
	created, err := svc.Drive.Files.Create(driveFile).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("creating ClassSetup spreadsheet via Drive: %w", err)
	}
	spreadsheetID = created.Id
	spreadsheetURL = fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", spreadsheetID)

	// 2. Rename the default sheet to "Students".
	//    New spreadsheets have one sheet with ID 0.
	_, err = svc.Sheets.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{{
			UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
				Properties: &sheets.SheetProperties{
					SheetId: 0,
					Title:   "Students",
				},
				Fields: "title",
			},
		}},
	}).Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("renaming sheet to Students: %w", err)
	}

	// 3. Write header + sample rows.
	_, err = svc.Sheets.Spreadsheets.Values.Update(spreadsheetID, "Students!A1", &sheets.ValueRange{
		Values: [][]interface{}{
			{"class", "student"},
			{"5A", "Emma Johnson"},
			{"5A", "Liam Smith"},
			{"5B", "Olivia Brown"},
			{"5B", "Noah Davis"},
		},
	}).ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("writing ClassSetup data: %w", err)
	}

	// 4. Bold the header row.
	_, err = svc.Sheets.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{{
			RepeatCell: &sheets.RepeatCellRequest{
				Range: &sheets.GridRange{
					SheetId:          0,
					StartRowIndex:    0,
					EndRowIndex:      1,
					StartColumnIndex: 0,
					EndColumnIndex:   2,
				},
				Cell: &sheets.CellData{
					UserEnteredFormat: &sheets.CellFormat{
						TextFormat: &sheets.TextFormat{Bold: true},
					},
				},
				Fields: "userEnteredFormat.textFormat.bold",
			},
		}},
	}).Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("formatting ClassSetup header: %w", err)
	}

	return spreadsheetID, spreadsheetURL, nil
}
