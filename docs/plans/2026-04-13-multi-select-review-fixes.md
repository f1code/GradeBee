# Address Code Review Feedback — Multi-Select Upload

## Goal
Fix issues identified in code review of `feat/multi-select-upload`.

## Changes

### 1. Extract shared batch upload helper (Important #1)
**File**: `frontend/src/components/AudioUpload.tsx`

Extract a helper function used by both `processFiles` and `handleDriveImport`:

```ts
async function runBatchUpload(
  items: { name: string; upload: () => Promise<unknown> }[],
  onProgress: (index: number, name: string) => void
): Promise<{ succeeded: number; failed: string[]; lastError: string | null }>
```

Both callers build the `items` array, call `runBatchUpload`, then handle the result with shared post-loop logic (set error/success state). The post-loop state updates are similar enough to also extract — evaluate during implementation.

### 2. Fix picker `enableFeature` double-call (Important #2)
**File**: `frontend/src/hooks/useDrivePicker.ts`

Break the builder chain into a `let builder = ...` variable. Call `.enableFeature(NAV_HIDDEN)` once, then conditionally `builder.enableFeature(MULTISELECT_ENABLED)` only when `multiSelect` is true.

### 3. Guard `picked[0]` in ReportExamples (Important #3)
**File**: `frontend/src/components/ReportExamples.tsx`

Change:
```ts
if (!picked) return
```
to:
```ts
if (!picked || picked.length === 0) return
```

### 4. Fix duplicate key on failedFiles list (Important #4)
**File**: `frontend/src/components/AudioUpload.tsx`

Change `key={f}` → `key={i}` (use index from `.map((f, i) => ...)`).

### 5. Replace inline style on error `<ul>` (Suggestion #5)
**File**: `frontend/src/components/AudioUpload.tsx`

Add a CSS class (e.g., `upload-error-list`) instead of inline `style={{ marginTop, paddingLeft }}`.

### 6. Pass plural title to Drive picker (Suggestion #6)
**File**: `frontend/src/components/AudioUpload.tsx`

In `handleDriveImport`, add `title: 'Select audio files'` to the `openPicker` options.

### 7. Add test for multi-file oversized rejection (Suggestion #7)
**File**: `frontend/src/components/__tests__/AudioUpload.test.tsx`

New test: select 2 files where 1 exceeds 25 MB → both rejected, error message shown, `uploadAudio` never called.
