// roster.go defines the Roster interface and its production implementation
// backed by Google Sheets. It centralises spreadsheet-ID resolution and data
// reads so that handlers don't depend on Sheets API details directly.
package handler

import (
	"context"
	"fmt"
	"strings"
)

// Roster abstracts read access to the user's student roster spreadsheet.
type Roster interface {
	ClassNames(ctx context.Context) ([]string, error)
	Students(ctx context.Context) ([]classGroup, error)
	SpreadsheetURL() string
}

// sheetsRoster reads roster data from a Google Sheets spreadsheet.
type sheetsRoster struct {
	svc           *googleServices
	spreadsheetID string
}

// newSheetsRoster resolves the user's spreadsheet ID from Clerk metadata,
// verifies the spreadsheet still exists via Drive.Files.Get, and returns a
// ready-to-use sheetsRoster. Returns a descriptive apiError when the
// spreadsheet is missing.
func newSheetsRoster(ctx context.Context, svc *googleServices) (*sheetsRoster, error) {
	meta, err := getGradeBeeMetadata(ctx, svc.User.UserID)
	if err != nil {
		return nil, fmt.Errorf("roster: metadata lookup failed: %w", err)
	}
	if meta == nil || meta.SpreadsheetID == "" {
		return nil, &apiError{
			Status:  404,
			Code:    "no_spreadsheet",
			Message: "ClassSetup spreadsheet not found. Try running setup again.",
		}
	}

	// Verify the spreadsheet still exists (drive.file scope allows Get on
	// files the app created).
	_, err = svc.Drive.Files.Get(meta.SpreadsheetID).Fields("id").Context(ctx).Do()
	if err != nil {
		return nil, &apiError{
			Status:  404,
			Code:    "no_spreadsheet",
			Message: "ClassSetup spreadsheet not found. Try running setup again.",
		}
	}

	return &sheetsRoster{svc: svc, spreadsheetID: meta.SpreadsheetID}, nil
}

// ClassNames reads class names from column A of the Students sheet,
// deduplicates them, and returns the unique list.
func (r *sheetsRoster) ClassNames(ctx context.Context) ([]string, error) {
	resp, err := r.svc.Sheets.Spreadsheets.Values.Get(r.spreadsheetID, "Students!A:A").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("roster: read class names: %w", err)
	}

	seen := make(map[string]struct{})
	var names []string
	for i, row := range resp.Values {
		if i == 0 { // skip header
			continue
		}
		if len(row) == 0 {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", row[0]))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names, nil
}

// Students reads columns A:B of the Students sheet and returns the roster
// grouped by class via parseStudentRows.
func (r *sheetsRoster) Students(ctx context.Context) ([]classGroup, error) {
	resp, err := r.svc.Sheets.Spreadsheets.Values.Get(r.spreadsheetID, "Students!A:B").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("roster: read students: %w", err)
	}

	var rows [][]interface{}
	if resp.Values != nil {
		rows = resp.Values
	}
	return parseStudentRows(rows)
}

// SpreadsheetURL returns the web URL for the underlying spreadsheet.
func (r *sheetsRoster) SpreadsheetURL() string {
	return fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", r.spreadsheetID)
}
