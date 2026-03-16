// google.go constructs authenticated Google Drive and Sheets API clients for
// the signed-in user and exposes shared helpers (Drive folder creation, API
// error types, JSON response writing) used across the handler package.
package handler

import (
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// googleServices holds authenticated Google API clients.
type googleServices struct {
	Drive  *drive.Service
	Sheets *sheets.Service
	User   *clerkUser
}

// newGoogleServices returns Drive + Sheets services for the authenticated user.
// Requires SessionClaims in context (set by RequireHeaderAuthorization middleware).
func newGoogleServices(r *http.Request) (*googleServices, error) {
	ctx := r.Context()
	claims, ok := clerk.SessionClaimsFromContext(ctx)
	if !ok || claims == nil {
		return nil, &apiError{Status: http.StatusForbidden, Err: nil, Code: "unauthorized", Message: "missing or invalid session"}
	}
	userID := claims.Subject
	accessToken, err := getGoogleOAuthToken(ctx, userID)
	if err != nil {
		return nil, &apiError{Status: http.StatusBadGateway, Err: err}
	}
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
	driveSrv, err := drive.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		loggerFromContext(ctx).Error("google services failed", "operation", "drive.NewService", "error", err)
		return nil, &apiError{Status: http.StatusInternalServerError, Err: err}
	}
	sheetsSrv, err := sheets.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		loggerFromContext(ctx).Error("google services failed", "operation", "sheets.NewService", "error", err)
		return nil, &apiError{Status: http.StatusInternalServerError, Err: err}
	}
	return &googleServices{Drive: driveSrv, Sheets: sheetsSrv, User: &clerkUser{UserID: userID}}, nil
}

// apiError is an error that carries an HTTP status code.
type apiError struct {
	Status  int
	Err     error
	Code    string // machine-readable error code, e.g. "no_spreadsheet"
	Message string // human-readable message
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

	resp := map[string]string{}
	switch {
	case err.Code != "":
		resp["error"] = err.Code
	case err.Err != nil:
		resp["error"] = err.Err.Error()
	default:
		resp["error"] = "unknown error"
	}
	if err.Message != "" {
		resp["message"] = err.Message
	}
	writeJSON(w, err.Status, resp)
}

// createFolder creates a folder in Drive. Uses drive.file scope (no listing).
func createFolder(srv *drive.Service, parentID, name string) (string, error) {
	folder := &drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}
	created, err := srv.Files.Create(folder).Fields("id").Do()
	if err != nil {
		return "", err
	}
	return created.Id, nil
}
