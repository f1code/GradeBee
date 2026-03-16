// clerk_metadata.go manages GradeBee-specific data (Google Drive folder and
// Sheets spreadsheet IDs) persisted in Clerk user private metadata. This
// avoids requiring the broad Drive listing scope by caching known resource IDs
// directly against the authenticated user's Clerk profile.
package handler

import (
	"context"
	"encoding/json"

	"github.com/clerk/clerk-sdk-go/v2/user"
)

const (
	metaKeyFolderID       = "gradebee_folder_id"
	metaKeySpreadsheetID  = "gradebee_spreadsheet_id"
	metaKeyUploadsID      = "gradebee_uploads_id"
	metaKeyNotesID        = "gradebee_notes_id"
	metaKeyReportsID      = "gradebee_reports_id"
)

// gradeBeeMetadata holds GradeBee Drive/Sheets IDs stored in Clerk user metadata.
type gradeBeeMetadata struct {
	FolderID      string `json:"gradebee_folder_id"`
	SpreadsheetID string `json:"gradebee_spreadsheet_id"`
	UploadsID     string `json:"gradebee_uploads_id"`
	NotesID       string `json:"gradebee_notes_id"`
	ReportsID     string `json:"gradebee_reports_id"`
}

// getGradeBeeMetadata retrieves GradeBee IDs from the user's Clerk private metadata.
func getGradeBeeMetadata(ctx context.Context, userID string) (*gradeBeeMetadata, error) {
	u, err := user.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(u.PrivateMetadata) == 0 {
		return nil, nil
	}
	var meta gradeBeeMetadata
	if err := json.Unmarshal(u.PrivateMetadata, &meta); err != nil {
		return nil, err
	}
	if meta.FolderID == "" && meta.SpreadsheetID == "" {
		return nil, nil
	}
	return &meta, nil
}

// setGradeBeeMetadata stores GradeBee IDs in the user's Clerk private metadata.
// Merges with existing metadata so other keys are preserved.
func setGradeBeeMetadata(ctx context.Context, userID string, meta *gradeBeeMetadata) error {
	// Build the merge payload: only the keys we want to set.
	// Clerk merges this with existing private metadata.
	merge := make(map[string]interface{})
	if meta.FolderID != "" {
		merge[metaKeyFolderID] = meta.FolderID
	}
	if meta.SpreadsheetID != "" {
		merge[metaKeySpreadsheetID] = meta.SpreadsheetID
	}
	if meta.UploadsID != "" {
		merge[metaKeyUploadsID] = meta.UploadsID
	}
	if meta.NotesID != "" {
		merge[metaKeyNotesID] = meta.NotesID
	}
	if meta.ReportsID != "" {
		merge[metaKeyReportsID] = meta.ReportsID
	}
	if len(merge) == 0 {
		return nil
	}
	raw, err := json.Marshal(merge)
	if err != nil {
		return err
	}
	rawMsg := json.RawMessage(raw)
	_, err = user.UpdateMetadata(ctx, userID, &user.UpdateMetadataParams{
		PrivateMetadata: &rawMsg,
	})
	return err
}
