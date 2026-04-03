// google_token.go handles the GET /google-token endpoint that returns the
// user's Google OAuth access token (via Clerk) so the frontend can use it
// with the Google Picker API.
package handler

import (
	"errors"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
)

// GoogleTokenResponse is the JSON response for GET /google-token.
type GoogleTokenResponse struct {
	AccessToken string `json:"accessToken"`
}

func handleGoogleToken(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		writeAPIError(w, r, &apiError{
			Status:  http.StatusForbidden,
			Code:    "unauthorized",
			Message: "missing or invalid session",
		})
		return
	}
	userID := claims.Subject

	token, err := getGoogleOAuthToken(r.Context(), userID)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeAPIError(w, r, ae)
			return
		}
		log.Error("google-token failed", "user_id", userID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to retrieve Google token"})
		return
	}

	log.Info("google-token returned", "user_id", userID)
	writeJSON(w, http.StatusOK, GoogleTokenResponse{AccessToken: token})
}
