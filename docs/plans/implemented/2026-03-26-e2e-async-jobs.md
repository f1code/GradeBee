# E2E Tests for Async Upload Jobs

## Goal

Add Playwright e2e specs covering the upload → async job processing → job status UI flow. All backend API calls are mocked via `page.route()` so tests run without real Google/OpenAI services.

## Background

- **Backend unit tests** already cover queue mechanics, pipeline steps, job listing, and retry (27 tests).
- **Frontend unit tests** already cover `JobStatus` (10 tests) and `AudioUpload` (13 tests) with vitest.
- **Missing:** e2e tests that exercise the full integrated flows through real browser interactions — upload triggering a job, polling showing progress, retry on failure, etc.

## New Files

### `e2e/upload-jobs.spec.ts`

Authenticated project (storageState from `global.setup.ts`). Mocks `GET /setup` to return `setupDone: true` so the app skips Drive setup. All tests mock `GET /students` to return a basic roster (so student list renders). Tests use `page.route()` to control `/upload`, `/drive-import`, `/jobs`, and `/jobs/retry` responses.

### `playwright.config.ts`

Add `upload-jobs.spec.ts` to the `authenticated` project's `testMatch` pattern.

## Test Cases

### 1. Upload success shows toast and triggers job polling

**Flow:** File upload → success toast → job appears in active list → transitions to done.

- Mock `POST /upload` → 200 `{ fileId: "f1", fileName: "recording.mp3" }`
- Mock `GET /jobs` to return sequence:
  - 1st call: `{ active: [{ fileId: "f1", fileName: "recording.mp3", status: "transcribing" }], failed: [], done: [] }`
  - 2nd call: `{ active: [], failed: [], done: [{ fileId: "f1", fileName: "recording.mp3", status: "done", noteUrls: ["https://docs.google.com/..."] }] }`
- Use `fileInputRef` via `page.setInputFiles()` on `[data-testid="file-input"]`
- Assert:
  - `[data-testid="upload-progress"]` appears with file name
  - `[data-testid="upload-success"]` toast appears ("Uploaded! Processing in background.")
  - `[data-testid="job-active"]` appears with "recording.mp3" and status label
  - Eventually `[data-testid="job-done"]` appears with "1 note created" and a link

### 2. Upload error shows error state and retry

**Flow:** Upload fails → error message → click "Try again" → back to idle.

- Mock `POST /upload` → 500 `{ error: "Drive API unavailable" }`
- Assert:
  - `[data-testid="upload-error"]` visible with error text
  - Click "Try again" button
  - `[data-testid="drop-zone"]` (or `[data-testid="mobile-upload"]`) reappears

### 3. Job status shows active jobs with progress labels

**Flow:** Page loads with jobs already in-flight.

- Mock `GET /jobs` → active jobs in different states
- Assert correct status labels per status:
  - `queued` → "Queued"
  - `transcribing` → "Transcribing"
  - `extracting` → "Analyzing transcript"
  - `creating_notes` → "Creating notes"

### 4. Job status shows failed jobs with retry

**Flow:** Failed jobs appear → click "Retry All" → jobs move back to active.

- Mock `GET /jobs` 1st call: `{ active: [], failed: [{ fileId: "f1", fileName: "bad.mp3", status: "failed", error: "Whisper timeout" }], done: [] }`
- Assert:
  - `[data-testid="job-failed-section"]` visible
  - `[data-testid="job-failed"]` shows "bad.mp3" and "Whisper timeout"
  - `[data-testid="job-retry-btn"]` shows "Retry All"
- Mock `POST /jobs/retry` → 200
- Mock `GET /jobs` subsequent: `{ active: [{ fileId: "f1", fileName: "bad.mp3", status: "queued" }], failed: [], done: [] }`
- Click retry button
- Assert:
  - Button shows "Retrying…" (disabled)
  - `[data-testid="job-failed-section"]` disappears
  - `[data-testid="job-active"]` appears with "bad.mp3"

### 5. Done jobs show note links and "new" badge

**Flow:** Completed job with note URLs renders links and the "new" badge.

- Mock `GET /jobs` → `{ active: [], failed: [], done: [{ fileId: "f1", fileName: "lesson.mp3", status: "done", noteUrls: ["https://docs.google.com/doc1", "https://docs.google.com/doc2"] }] }`
- Assert:
  - `[data-testid="job-done"]` visible
  - "2 notes created" text
  - Two "Open note" links with correct `href` and `target="_blank"`
  - `[data-testid="job-new-badge"]` visible (click dismisses it)

### 6. Empty job list renders nothing

**Flow:** No jobs at all → JobStatus component doesn't render.

- Mock `GET /jobs` → `{ active: [], failed: [], done: [] }`
- Assert `[data-testid="job-status"]` is not visible

### 7. Job polling error shows error message

**Flow:** `/jobs` endpoint fails → error message displayed.

- Mock `GET /jobs` → 500 `{ error: "queue unavailable" }`
- Assert `[data-testid="job-error"]` visible with error text

## Implementation Notes

### Controlling poll timing

The `JobStatus` component polls on a timer (3s active, 15s idle). For tests that need to observe state transitions, use a counter on the mocked `GET /jobs` route to return different responses on successive calls. Use `page.waitForResponse()` or `expect(...).toBeVisible()` with timeouts rather than artificial delays.

### Shared setup helper

All tests in this file need the same base mocking (setup check + students). Extract to a helper:

```ts
async function mockAuthenticatedApp(page: Page) {
  await page.route('**/setup', route =>
    route.fulfill({ status: 200, body: JSON.stringify({ setupDone: true }) })
  )
  await page.route('**/students', route =>
    route.fulfill({
      status: 200,
      body: JSON.stringify({
        spreadsheetUrl: 'https://docs.google.com/spreadsheets/d/abc/edit',
        classes: [{ name: '5A', students: [{ name: 'Emma' }] }],
      }),
    })
  )
}
```

### File upload simulation

Playwright supports `page.setInputFiles()` on hidden `<input type="file">` elements. Use a small buffer or fixture file:

```ts
await page.getByTestId('file-input').setInputFiles({
  name: 'recording.mp3',
  mimeType: 'audio/mpeg',
  buffer: Buffer.from('fake-audio'),
})
```

### Config change

In `playwright.config.ts`, update the authenticated project's `testMatch`:

```ts
testMatch: /(drive-setup|students|upload-jobs)\.spec\.ts/,
```

## Decisions

1. **Drive import flow** — Skip in automated tests. The Google Drive picker is a real Google widget that can't be meaningfully mocked.
2. **Poll timing** — No configuration. Use Playwright's built-in `expect(...).toBeVisible()` with timeouts to wait for state transitions naturally.
