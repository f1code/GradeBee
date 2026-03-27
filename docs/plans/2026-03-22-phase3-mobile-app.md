# Phase 3: Mobile App — Detailed Implementation Plan

## Goal

Build an Expo React Native app (Android only) that registers as an OS share target, authenticates via Clerk, uploads shared audio files to the GradeBee backend, and shows job status with retry. Relies on the NATS pipeline (Phase 1) and existing `GET /jobs`, `POST /jobs/retry` endpoints (Phase 2).

> **Scope: Android only.** No Apple Developer account available. iOS can be added later with minimal changes (add `expo-share-intent` iOS config + EAS iOS build profile).

## Prerequisites (from Phases 1 & 2)

- NATS stream `UPLOADS` + KV bucket `UPLOAD_JOBS` operational
- `GET /jobs` and `POST /jobs/retry` endpoints live
- `upload_process.go` consumer pipeline working (transcribe → extract → auto-create notes)

---

## Backend: `share_upload.go`

### New endpoint: `POST /share-upload`

Single new file. Accepts multipart/form-data audio from the mobile app, saves to Drive, publishes to NATS.

**`backend/share_upload.go`**

```
func handleShareUpload(w http.ResponseWriter, r *http.Request) {
```

- Parse multipart form, 25 MB max (`r.ParseMultipartForm(25 << 20)`)
- Read `file` field from form
- Validate MIME type starts with `audio/` (reject otherwise with 415)
- Detect/fix audio format using existing `audio_format.go` helpers (magic byte detection, 3GP patch, extension fix)
- Prepend ISO date to filename: `2026-03-22-recording.m4a`
- Get `googleServices` via `serviceDeps.GoogleServices(r)` (Clerk JWT → Google OAuth token)
- Get `DriveStore` via `serviceDeps.GetDriveStore(svc)`
- Upload to user's Drive `uploads/` folder (same as `handleUpload` in `upload.go`)
- Get `UploadQueue` via `serviceDeps.GetUploadQueue()`
- Publish job to NATS with `source: "mobile"`
- Return `{ fileId, fileName, status: "queued" }` (200)

Error cases:
- No file → 400
- Non-audio MIME → 415
- File too large → 413
- Drive upload failure → 500 (wrapped `apiError`)

**`backend/handler.go`** — add route:

```go
case r.Method == http.MethodPost && r.URL.Path == "/share-upload":
    authMiddleware(handleShareUpload).ServeHTTP(w, r)
```

**`backend/share_upload_test.go`** — test with stub deps, multipart audio file.

### Why separate from `/upload`?

`/upload` may evolve differently (Drive picker, batch upload). Keeping mobile-specific concerns (MIME validation, size limit messaging) separate is cleaner. Both publish to the same NATS stream.

---

## Mobile App: `mobile/`

### Project setup

```
mobile/
├── app.json
├── package.json
├── tsconfig.json
├── babel.config.js
├── App.tsx
├── eas.json
├── src/
│   ├── auth/
│   │   ├── ClerkProvider.tsx
│   │   └── tokenCache.ts
│   ├── screens/
│   │   ├── LoginScreen.tsx
│   │   ├── ShareScreen.tsx
│   │   └── QueueScreen.tsx
│   ├── api/
│   │   ├── client.ts
│   │   ├── upload.ts
│   │   ├── jobs.ts
│   │   └── retry.ts
│   └── components/
│       ├── JobList.tsx
│       └── StatusBadge.tsx
```

### File-by-file

**`mobile/package.json`**

Key dependencies:
- `expo` (~52)
- `expo-router` — file-based routing
- `@clerk/clerk-expo` — Clerk auth SDK for Expo
- `expo-secure-store` — secure token persistence
- `expo-share-intent` — receive share intents on Android
- `expo-file-system` — read shared file URI for upload
- `react-native-safe-area-context`, `react-native-screens` — navigation basics

Dev dependencies:
- `typescript`, `@types/react`
- `expo-dev-client` (for dev builds with native modules)

**`mobile/app.json`**

```json
{
  "expo": {
    "name": "GradeBee",
    "slug": "gradebee",
    "scheme": "gradebee",
    "version": "1.0.0",
    "orientation": "portrait",
    "icon": "./assets/icon.png",
    "splash": { "image": "./assets/splash.png" },
    "ios": {
      "bundleIdentifier": "com.gradebee.app",
      "supportsTablet": false
    },
    "android": {
      "package": "com.gradebee.app",
      "intentFilters": [
        {
          "action": "android.intent.action.SEND",
          "category": ["android.intent.category.DEFAULT"],
          "data": [{ "mimeType": "audio/*" }]
        }
      ]
    },
    "plugins": [
      "expo-router",
      "expo-secure-store",
      [
        "expo-share-intent",
        {
          "androidIntentFilters": ["audio/*"]
        }
      ]
    ]
  }
}
```

**Share target registration details:**

- **Android:** The `intentFilters` in `app.json` generates the `<intent-filter>` in `AndroidManifest.xml` for `ACTION_SEND` + `audio/*`. No config plugin needed — Expo handles this natively.
- **iOS:** Deferred (no Apple Developer account). When ready, add `expo-share-intent` plugin with `iosActivationRules` for Share Extension.

**`mobile/eas.json`**

```json
{
  "cli": { "version": ">= 5.0.0" },
  "build": {
    "development": {
      "developmentClient": true,
      "distribution": "internal",
      "android": { "buildType": "apk" }
    },
    "preview": {
      "distribution": "internal",
      "android": { "buildType": "apk" }
    },
    "production": {
      "android": {}
    }
  },
  "submit": {
    "production": {
      "android": { "serviceAccountKeyPath": "./google-services.json" }
    }
  }
}
```

**`mobile/src/auth/tokenCache.ts`**

```ts
// Clerk token cache backed by expo-secure-store
import * as SecureStore from 'expo-secure-store'
import { TokenCache } from '@clerk/clerk-expo'

export const tokenCache: TokenCache = {
  async getToken(key: string) {
    return SecureStore.getItemAsync(key)
  },
  async saveToken(key: string, value: string) {
    return SecureStore.setItemAsync(key, value)
  },
}
```

**`mobile/src/auth/ClerkProvider.tsx`**

- Wraps `<ClerkProvider publishableKey={CLERK_PK} tokenCache={tokenCache}>`
- `CLERK_PK` from `app.json` extra or env via `expo-constants`

**`mobile/App.tsx`**

- `<ClerkProvider>` at root
- Conditional render: `useAuth().isSignedIn` → `<QueueScreen>` (default), else `<LoginScreen>`
- `useShareIntent()` hook from `expo-share-intent` — when share data present, navigate to `<ShareScreen>`

**`mobile/src/screens/LoginScreen.tsx`**

- Clerk `<SignIn>` or `useSignIn()` with Google OAuth strategy (`strategy: "oauth_google"`)
- Single "Sign in with Google" button
- On success, Clerk session persisted via `expo-secure-store`
- Minimal UI: app logo + sign-in button

**`mobile/src/screens/ShareScreen.tsx`**

- Receives shared file URI from `useShareIntent()`
- Shows: filename, file size, "Upload" button
- On upload:
  1. Read file via `expo-file-system`
  2. Get Clerk session token via `useAuth().getToken()`
  3. `POST /share-upload` with multipart form (file + auth header `Authorization: Bearer <token>`)
  4. Show success → navigate to QueueScreen
  5. On error → show error message + retry button
- If not signed in when share arrives → redirect to LoginScreen, then back to ShareScreen after auth

**`mobile/src/screens/QueueScreen.tsx`**

- Default screen (app home)
- Calls `GET /jobs` on mount + pull-to-refresh
- Two sections:
  - **Processing** — jobs with status `queued|transcribing|extracting|creating_notes` (show `StatusBadge` per job)
  - **Failed** — jobs with `status: "failed"` (show filename, error, failedAt)
- "Retry All" button → `POST /jobs/retry` → refresh list
- **Done** jobs: show briefly with checkmark, then fade (or show count: "3 recordings processed today")
- Empty state: "Share an audio recording to get started" with illustration

**`mobile/src/api/client.ts`**

- Base URL from env/config (e.g. `https://api.gradebee.com`)
- Helper: `authFetch(path, options, getToken)` — attaches `Authorization: Bearer` header
- Handles 401 → sign out (session expired)

**`mobile/src/api/upload.ts`**

```ts
export async function shareUpload(fileUri: string, fileName: string, getToken: () => Promise<string>): Promise<{ fileId: string; fileName: string; status: string }>
```

- Builds `FormData` with file URI (React Native handles file:// URIs in FormData natively)
- `POST /share-upload`

**`mobile/src/api/jobs.ts`**

```ts
export async function listJobs(getToken): Promise<{ active: Job[]; failed: Job[]; done: Job[] }>
```

- `GET /jobs`

**`mobile/src/api/retry.ts`**

```ts
export async function retryFailed(getToken): Promise<{ retriedCount: number }>
```

- `POST /jobs/retry`

**`mobile/src/components/JobList.tsx`**

- FlatList rendering jobs grouped by section
- Each item: filename, StatusBadge, timestamp
- Failed items: error message in red, smaller text

**`mobile/src/components/StatusBadge.tsx`**

- Colored pill: blue (queued), yellow (transcribing/extracting), green (done), red (failed)
- Maps status string to label + color

---

## Auth Flow

```
1. User opens app (or share intent triggers app)
2. ClerkProvider checks expo-secure-store for cached session
3. If no session → LoginScreen → "Sign in with Google" → Clerk OAuth flow → session cached
4. If session exists → proceed
5. All API calls: useAuth().getToken() → Bearer token in Authorization header
6. Backend: Clerk JWT middleware validates token, extracts userId
7. Backend: getGoogleOAuthToken(ctx, userId) → Google Drive access (same as web)
```

Key point: the mobile app uses the **same Clerk project** as the web app. Same users, same sessions, same Google OAuth connection. No additional OAuth setup needed.

---

## Environment / Config

**Mobile app env vars** (via `eas.json` env or `app.config.js`):

| Variable | Purpose |
|----------|---------|
| `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` | Clerk frontend key |
| `EXPO_PUBLIC_API_URL` | Backend URL (`https://api.gradebee.com`) |

**Backend** — no new env vars beyond Phase 1 (`NATS_URL`, `NATS_CREDS`, `PROCESS_SECRET`).

---

## EAS Build Setup

1. `npm install -g eas-cli`
2. `cd mobile && eas init` — links to Expo project
3. `eas build --platform android --profile preview` — builds APK for internal distribution
4. Android: signing key auto-generated by EAS on first build

### EAS Build Setup

1. `npm install -g eas-cli`
2. `cd mobile && eas init` — links to Expo project
3. `eas build --platform android --profile preview` — builds APK for internal distribution
4. Android: signing key auto-generated by EAS on first build

### Distribution

- Android: `eas submit --platform android` → Play Store internal track (requires service account JSON)
- For initial testing: use `preview` profile with internal distribution (direct APK install)

---

## CORS / Backend Config

The mobile app makes direct HTTP calls to the backend (not browser-based), so CORS is irrelevant. However, the backend currently sets CORS headers — no changes needed, the headers are simply ignored by React Native's fetch.

---

## Summary of Changes

| Area | File | Change |
|------|------|--------|
| Backend | `share_upload.go` | New — `POST /share-upload` handler |
| Backend | `share_upload_test.go` | New — tests |
| Backend | `handler.go` | Add `/share-upload` route |
| Backend | `ARCHITECTURE.md` | Add `/share-upload` to route table |
| Mobile | `mobile/` (entire directory) | New — Expo project |

---

## Open Questions

1. **App icon / splash screen** — reuse web favicon/logo or design new assets?
2. **Offline behavior** — if user shares audio while offline, should we queue locally and upload when back online? Initial plan: show error "No internet connection", user retries manually. Local queue is a future enhancement.
3. **Android: which audio apps support sharing?** — Voice Recorder, Samsung Voice Recorder, and file managers all use `ACTION_SEND` with `audio/*`. Google Recorder uses a custom export flow. Need to test on target devices.
4. **Clerk Expo SDK version** — verify `@clerk/clerk-expo` supports the Clerk project's API version. Should be fine with latest.
5. **`expo-share-intent` vs raw Android intent** — `expo-share-intent` adds iOS Share Extension complexity we don't need yet. Evaluate whether a minimal Expo config plugin (just the `intentFilters` in `app.json` + reading intent data via `expo-linking` or `Linking.getInitialURL`) is simpler than pulling in the full `expo-share-intent` package.

## Effort Estimate

| Task | Estimate |
|------|----------|
| Backend: `share_upload.go` + test + route | 1–2 hours |
| Mobile: Expo project init + config | 1 hour |
| Mobile: Clerk auth + SecureStore + LoginScreen | 2–3 hours |
| Mobile: Share intent handling + ShareScreen | 3–4 hours |
| Mobile: API client + upload/jobs/retry | 1–2 hours |
| Mobile: QueueScreen + JobList + StatusBadge | 3–4 hours |
| EAS Build setup (Android) + first build | 1–2 hours |
| Device testing (Android) | 1–2 hours |
| **Total** | **~2 days** |
