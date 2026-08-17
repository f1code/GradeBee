// auth.go provides helpers for retrieving authenticated user information from
// Clerk: the Google OAuth access token for Drive/Sheets, and org-level helpers
// for extracting the active Clerk Organization ID and role (Phase 1 tenancy).
package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

// groupIDFromRequest extracts the active Clerk Organization ID from the
// verified JWT session claims. Returns a 403 "unauthorized" error if claims
// are missing, or a 403 "no_active_org" error if no organisation is active
// (e.g. the user hasn't joined a Group yet).
func groupIDFromRequest(r *http.Request) (string, error) {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return "", &apiError{Status: http.StatusForbidden, Code: "unauthorized", Message: "missing or invalid session"}
	}
	if claims.ActiveOrganizationID == "" {
		return "", &apiError{Status: http.StatusForbidden, Code: "no_active_org", Message: "no active organization \u2014 ask your admin for an invitation"}
	}
	return claims.ActiveOrganizationID, nil
}

// isAdmin reports whether the request carries an admin role for the active
// organisation. Returns false if claims are absent or the role is not
// "org:admin".
func isAdmin(r *http.Request) bool {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return false
	}
	return claims.HasRole("org:admin")
}

// getGoogleOAuthToken retrieves the Google OAuth access token for a user from Clerk.
func getGoogleOAuthToken(ctx context.Context, userID string) (string, error) {
	log := loggerFromContext(ctx)
	clerkSecretKey := os.Getenv("CLERK_SECRET_KEY")
	if clerkSecretKey == "" {
		log.Error("oauth token fetch failed", "user_id", userID, "reason", "CLERK_SECRET_KEY not configured")
		return "", fmt.Errorf("CLERK_SECRET_KEY not configured")
	}

	list, err := user.ListOAuthAccessTokens(ctx, &user.ListOAuthAccessTokensParams{
		ID:       userID,
		Provider: "oauth_google",
	})
	if err != nil {
		log.Error("oauth token fetch failed", "user_id", userID, "reason", "list oauth tokens", "error", err)
		return "", fmt.Errorf("fetching oauth token: %w", err)
	}
	if list == nil || len(list.OAuthAccessTokens) == 0 {
		log.Warn("oauth token fetch failed", "user_id", userID, "reason", "no token found")
		return "", fmt.Errorf("no Google OAuth token found — user may need to reconnect")
	}

	return list.OAuthAccessTokens[0].Token, nil
}
