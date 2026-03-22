# Validate Clerk OAuth Token Offline Access

## Goal

Confirm that Clerk's `user.ListOAuthAccessTokens` returns a valid Google OAuth access token when called server-side **without** an active user session — i.e., can a background job use this API to get a working Google Drive token hours/days after the user last logged in?

## Plan

Write a small Go CLI tool (`backend/cmd/check-token/main.go`) that:

1. Loads `.env` (for `CLERK_SECRET_KEY`)
2. Inits the Clerk SDK
3. Takes a Clerk user ID as a CLI arg
4. Calls `user.ListOAuthAccessTokens` — same code path as `auth.go`
5. If a token is returned, makes a quick Google Drive `files.list` call to confirm it's valid
6. Prints results: token present? token works? expiry?

### Test procedure

1. Log into GradeBee web app normally (ensures Clerk has a Google OAuth connection)
2. Close the browser / sign out — no active session
3. Wait a few minutes (or hours for a stronger test)
4. Run the CLI tool with your user ID
5. Check if it returns a valid, working token

### What the results tell us

| Token returned? | Drive call works? | Meaning |
|----------------|-------------------|---------|
| Yes | Yes | **We're good.** Clerk auto-refreshes. No changes needed. |
| Yes | No (401) | Clerk returns a stale/expired token. We need to store refresh tokens ourselves. |
| No | — | Clerk purges tokens when session ends. We need our own OAuth flow + refresh token storage. |

## Files

**`backend/cmd/check-token/main.go`** — single file, ~50 lines

```go
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
    godotenv.Load("../../.env")
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
```

## Effort

~30 minutes to write, build, and run.
