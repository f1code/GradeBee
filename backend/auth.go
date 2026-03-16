package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// clerkUser represents the essential info extracted from Clerk.
type clerkUser struct {
	UserID string
}

// authenticateRequest validates the Bearer token with Clerk Backend API
// and returns the user ID.
func authenticateRequest(r *http.Request) (*clerkUser, error) {
	log := loggerFromRequest(r)

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		log.Warn("auth failed", "reason", "missing Authorization header")
		return nil, fmt.Errorf("missing Authorization header")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		log.Warn("auth failed", "reason", "invalid Authorization header format")
		return nil, fmt.Errorf("invalid Authorization header format")
	}

	// Verify the session token with Clerk
	clerkSecretKey := os.Getenv("CLERK_SECRET_KEY")
	if clerkSecretKey == "" {
		log.Error("auth failed", "reason", "CLERK_SECRET_KEY not configured")
		return nil, fmt.Errorf("CLERK_SECRET_KEY not configured")
	}

	// Use Clerk Backend API to verify the token and get user info
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://api.clerk.com/v1/tokens/verify", strings.NewReader(`{"token":"`+token+`"}`))
	if err != nil {
		log.Error("auth failed", "reason", "creating verify request", "error", err)
		return nil, fmt.Errorf("creating verify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+clerkSecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("auth failed", "reason", "verifying token", "error", err)
		return nil, fmt.Errorf("verifying token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn("auth failed", "reason", "token verification failed", "status", resp.StatusCode)
		return nil, fmt.Errorf("token verification failed (status %d)", resp.StatusCode)
	}

	var result struct {
		Sub string `json:"sub"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Error("auth failed", "reason", "decoding verify response", "error", err)
		return nil, fmt.Errorf("decoding verify response: %w", err)
	}

	return &clerkUser{UserID: result.Sub}, nil
}

// getGoogleOAuthToken retrieves the Google OAuth access token for a user from Clerk.
func getGoogleOAuthToken(ctx context.Context, userID string) (string, error) {
	log := loggerFromContext(ctx)
	clerkSecretKey := os.Getenv("CLERK_SECRET_KEY")

	url := fmt.Sprintf("https://api.clerk.com/v1/users/%s/oauth_access_tokens/oauth_google", userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		log.Error("oauth token fetch failed", "user_id", userID, "reason", "creating request", "error", err)
		return "", fmt.Errorf("creating oauth request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+clerkSecretKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("oauth token fetch failed", "user_id", userID, "reason", "fetching", "error", err)
		return "", fmt.Errorf("fetching oauth token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn("oauth token fetch failed", "user_id", userID, "status", resp.StatusCode)
		return "", fmt.Errorf("failed to get oauth token (status %d)", resp.StatusCode)
	}

	var tokens []struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		log.Error("oauth token fetch failed", "user_id", userID, "reason", "decoding response", "error", err)
		return "", fmt.Errorf("decoding oauth response: %w", err)
	}
	if len(tokens) == 0 {
		log.Warn("oauth token fetch failed", "user_id", userID, "reason", "no token found")
		return "", fmt.Errorf("no Google OAuth token found — user may need to reconnect")
	}

	return tokens[0].Token, nil
}
