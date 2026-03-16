// auth.go provides helpers for retrieving authenticated user information from
// Clerk, specifically the Google OAuth access token required to call Google
// Drive and Sheets APIs on behalf of the signed-in user.
package handler

import (
	"context"
	"fmt"
	"os"

	"github.com/clerk/clerk-sdk-go/v2/user"
)

// clerkUser represents the essential info extracted from Clerk.
type clerkUser struct {
	UserID string
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
