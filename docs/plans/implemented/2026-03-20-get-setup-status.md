# GET /setup — Check setup status from Clerk metadata

## Goal

The frontend currently uses localStorage to track whether Drive setup is complete. This breaks across devices/browsers. Add a `GET /setup` endpoint that checks Clerk metadata so the frontend can determine setup status server-side.

## Proposed Changes

### Backend

- **`backend/handler.go`** — Register `GET /setup` route alongside existing `POST /setup`
- **`backend/setup.go`** — Add `handleGetSetup` handler:
  - Calls `getGradeBeeMetadata(ctx, userID)`
  - Returns `{ "setupDone": true, "folderId": "...", "spreadsheetId": "..." }` if metadata has a `FolderID`
  - Returns `{ "setupDone": false }` otherwise
  - No Drive API calls — just reads Clerk metadata

### Frontend

- **`frontend/src/App.tsx`** — On sign-in, call `GET /setup` instead of reading localStorage. Remove `SETUP_DONE_KEY` localStorage usage. Use the response to set `setupDone` state.
- **`frontend/src/api.ts`** (if needed) — Add `getSetupStatus()` helper

## Decisions

- No localStorage caching. Always hit the backend for setup status.
