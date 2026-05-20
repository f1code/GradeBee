// google.go provides shared HTTP error helpers and a Drive-read-only client
// constructor for the /drive-import endpoint. The full Google Sheets/Docs
// clients have been removed — all data is now in SQLite.
package handler

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// apiError is an error that carries an HTTP status code.
type apiError struct {
	Status  int
	Err     error
	Code    string            // machine-readable error code, e.g. "no_spreadsheet"
	Message string            // human-readable message
	Details map[string]string // optional structured details (forward-compat)
}

func (e *apiError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// writeAPIError writes an apiError as a JSON response and logs it.
func writeAPIError(w http.ResponseWriter, r *http.Request, err *apiError) {
	log := getLogger()
	if r != nil {
		log = loggerFromRequest(r)
	}
	log.Warn("api error", "status", err.Status, "code", err.Code, "message", err.Message, "error", err.Err)

	type errorResponse struct {
		Error   string            `json:"error"`
		Message string            `json:"message,omitempty"`
		Details map[string]string `json:"details,omitempty"`
	}
	resp := errorResponse{}
	switch {
	case err.Code != "":
		resp.Error = err.Code
	case err.Err != nil:
		resp.Error = err.Err.Error()
	default:
		resp.Error = "unknown error"
	}
	if err.Message != "" {
		resp.Message = err.Message
	}
	if len(err.Details) > 0 {
		resp.Details = err.Details
	}
	writeJSON(w, err.Status, resp)
}

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
