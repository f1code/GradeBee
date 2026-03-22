package main

import (
	"context"
	"fmt"
	"os"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func main() {
	// Load .env - ignore missing file errors
	if err := godotenv.Load("../../.env"); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	if err := godotenv.Load("../.env"); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	clerk.SetKey(os.Getenv("CLERK_SECRET_KEY"))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: check-token <clerk-user-id>")
		os.Exit(1)
	}
	userID := os.Args[1]
	ctx := context.Background()

	// Step 1: Get token from Clerk
	list, err := user.ListOAuthAccessTokens(ctx, &user.ListOAuthAccessTokensParams{
		ID:       userID,
		Provider: "oauth_google",
	})
	if err != nil {
		fmt.Printf("❌ ListOAuthAccessTokens failed: %v\n", err)
		os.Exit(1)
	}
	if list == nil || len(list.OAuthAccessTokens) == 0 {
		fmt.Println("❌ No Google OAuth token found")
		os.Exit(1)
	}

	token := list.OAuthAccessTokens[0].Token
	fmt.Printf("✅ Token returned (%d chars)\n", len(token))

	// Step 2: Test token against Drive API
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	svc, err := drive.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		fmt.Printf("❌ Drive client creation failed: %v\n", err)
		os.Exit(1)
	}

	about, err := svc.About.Get().Fields("user/displayName").Do()
	if err != nil {
		fmt.Printf("✅ Token returned but ❌ Drive API call failed: %v\n", err)
		fmt.Println("→ Clerk returns stale tokens. Need own refresh token storage.")
		os.Exit(1)
	}

	fmt.Printf("✅ Drive API works! User: %s\n", about.User.DisplayName)
	fmt.Println("→ Clerk auto-refreshes tokens. Background processing will work as-is.")
}
