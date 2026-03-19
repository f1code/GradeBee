# Phase 6: Add from Drive — Implementation Plan

## Goal

Let users pick audio files already on their Google Drive (e.g. uploaded from phone) via Google Picker, then process them through the existing transcribe → extract → notes pipeline without re-uploading.

---

## Overview

The existing flow is: **upload file → POST /upload → POST /transcribe → POST /extract → confirm → POST /notes**.

For "Add from Drive", we skip the upload step. Google Picker grants per-file `drive.file` access to selected files. The backend can read them directly. We add a thin backend endpoint to optionally copy files into `GradeBee/uploads/` (for bookkeeping) and then the frontend drives the same transcribe→extract→notes pipeline.

---

## Proposed Changes

### 1. Frontend: Google Picker Integration

**New file: `frontend/src/hooks/useDrivePicker.ts`**

- Custom hook that loads the Google Picker API (`https://apis.google.com/js/api.js`)
- Opens Picker filtered to audio MIME types (`audio/*`)
- Returns selected file IDs and names
- Needs the Google OAuth token from Clerk (not the Clerk JWT) — requires a new backend endpoint or Clerk frontend SDK method to get the Google access token
- Uses `google.picker.PickerBuilder` with `ViewId.DOCS` filtered by MIME type

**Dependencies:**
- Google API client library (loaded via script tag, not npm — Picker API is not available as npm package)
- Google Client ID (already configured in Clerk; needs to be exposed as `VITE_GOOGLE_CLIENT_ID` env var)
- Google OAuth access token for the current user

### 2. Frontend: Get Google OAuth Token

**Problem:** Google Picker needs a raw Google OAuth access token. Clerk stores this server-side. The frontend Clerk SDK doesn't expose it directly.

**Solution — new backend endpoint: `GET /google-token`**

This endpoint returns the user's Google OAuth access token (retrieved from Clerk the same way other endpoints do). The frontend calls this before opening the Picker.

**File: `backend/google_token.go`**
```go
func handleGoogleToken(w http.ResponseWriter, r *http.Request) {
    token, err := getGoogleOAuthToken(r.Context(), userID)
    writeJSON(w, 200, map[string]string{"accessToken": token.AccessToken})
}
```

**File: `backend/handler.go`** — add route `GET /google-token`

**File: `frontend/src/api.ts`** — add `getGoogleToken()` function

### 3. Frontend: "Add from Drive" Button in AudioUpload

**File: `frontend/src/components/AudioUpload.tsx`**

- Add an "Add from Drive" button below the drop zone (secondary button style, with a Google Drive icon)
- On click: call `GET /google-token`, then open Picker
- On file selection: call `POST /drive-import` with file IDs, then continue with transcribe → extract → notes (same as current flow starting from the transcribing state)
- Support single file selection (keep it simple; multi-file is a future enhancement)

### 4. Backend: Drive Import Endpoint

**New file: `backend/drive_import.go`**

**`POST /drive-import`** (auth required)

Request:
```json
{
  "fileId": "...",
  "fileName": "recording.m4a"
}
```

Behavior:
1. Validate the file is accessible (Drive API `Files.Get` on the provided fileId)
2. Validate it's an audio file (check MIME type from Drive metadata)
3. Copy the file into `GradeBee/uploads/` using `Drive.Files.Copy` (keeps a record, ensures the app retains access even if the original is moved/deleted)
4. Return the **new copy's** file ID

Response:
```json
{
  "fileId": "...copy-id...",
  "fileName": "recording.m4a"
}
```

This is intentionally similar to the `/upload` response so the frontend can feed it into the same downstream flow.

**File: `backend/handler.go`** — add route `POST /drive-import`

**File: `backend/drive_store.go`** — add `Copy(ctx, fileID, destFolderID) (string, error)` to `DriveStore` interface + implementation

### 5. Frontend API Addition

**File: `frontend/src/api.ts`**

```ts
export async function importFromDrive(
  fileId: string,
  fileName: string,
  getToken: () => Promise<string | null>
): Promise<{ fileId: string; fileName: string }>

export async function getGoogleToken(
  getToken: () => Promise<string | null>
): Promise<{ accessToken: string }>
```

### 6. Backend: ARCHITECTURE.md Update

Add the two new routes to the routing table and document `DriveStore.Copy`.

---

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `backend/handler.go` | Edit | Add `GET /google-token` and `POST /drive-import` routes |
| `backend/google_token.go` | **New** | Handler returning user's Google OAuth access token |
| `backend/drive_import.go` | **New** | Handler: validate + copy Drive file to uploads folder |
| `backend/drive_import_test.go` | **New** | Tests for drive-import endpoint |
| `backend/drive_store.go` | Edit | Add `Copy` method to interface + impl |
| `backend/ARCHITECTURE.md` | Edit | Document new routes and Copy method |
| `frontend/src/api.ts` | Edit | Add `getGoogleToken` and `importFromDrive` functions |
| `frontend/src/hooks/useDrivePicker.ts` | **New** | Hook to load & open Google Picker |
| `frontend/src/components/AudioUpload.tsx` | Edit | Add "Add from Drive" button + Picker flow |
| `frontend/index.html` | Edit | Add Google API script tag |

---

## Implementation Order

1. **Backend: `GET /google-token`** — simplest, unblocks Picker work
2. **Backend: `DriveStore.Copy` + `POST /drive-import`** — file copy logic + tests
3. **Frontend: `useDrivePicker` hook** — Picker API loading + opening
4. **Frontend: `AudioUpload` integration** — wire button → Picker → import → existing pipeline
5. **Backend: ARCHITECTURE.md update**

---

## Open Questions

1. **Google Client ID env var** — ✅ Resolved. Clerk does not expose the configured Google OAuth Client ID via any API. Use `VITE_GOOGLE_CLIENT_ID` env var in the frontend (same Client ID configured in Clerk's Google social connection + Google Cloud Console). This is a static value, standard for Picker integrations.
2. **Token expiry** — ✅ Resolved. Clerk's `ListOAuthAccessTokens` returns a fresh token each time (Clerk handles refresh internally). Picker sessions are short-lived, well within the ~1hr token lifetime. If the Picker errors due to an expired token, the frontend can simply call `GET /google-token` again and re-open — no refresh token logic needed on our side.
3. **Multi-file selection** — ✅ Resolved. Single file only. The existing pipeline is synchronous (upload → transcribe → extract → confirm → save). Multi-file would require a batch UI and parallel processing — out of scope. Users can pick one file at a time, same as the current upload flow.
4. **Notification when note is ready** — ✅ Resolved. Not needed. The flow is synchronous — the user stays on the page and sees each step complete. Notifications only matter with background/async processing, which we don't have. Deferred indefinitely.
5. **Error handling / retry** — ✅ Resolved. Standard error handling only: show error message, user clicks to retry manually. No automatic retry logic or rate limiting in v1. Can be added at the API gateway level later if needed.
