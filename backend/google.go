// google.go provides the Drive-read-only client constructor for the
// /drive-import endpoint. The full Google Sheets/Docs
// clients have been removed — all data is now in SQLite.
package handler

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// newDriveReadClient returns a Drive-read-only client for the given user.
// Used only by /drive-import to download files from Google Drive.
func newDriveReadClient(ctx context.Context, userID string) (*drive.Service, error) {
	accessToken, err := getGoogleOAuthToken(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("drive client for user %s: %w", userID, err)
	}
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
	driveSvc, err := drive.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("drive client: %w", err)
	}
	return driveSvc, nil
}
