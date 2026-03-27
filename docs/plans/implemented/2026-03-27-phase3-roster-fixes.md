# Phase 3 Roster CRUD — Fix-ups

**Goal:** Address review findings from the Phase 3 roster CRUD implementation.

## 1. Empty state cancel button

| File | Change |
|------|--------|
| `frontend/src/components/AddClassForm.tsx` | Make `onCancel` optional (`onCancel?: () => void`). Hide Cancel button when not provided. |
| `frontend/src/components/StudentList.tsx` | Empty-state `AddClassForm`: drop `onCancel` prop entirely. |

## 2. User feedback on mutation errors

Add a lightweight toast/flash for transient errors. Avoids a heavy toast library — a simple self-dismissing message anchored at the bottom of `.student-list`.

| File | Change |
|------|--------|
| `frontend/src/components/StudentList.tsx` | Add `flashError: string \| null` state + 3s auto-clear timeout. Set it in catch blocks for `handleRenameClass`, `handleRenameStudent`, `handleDeleteClass`, `handleDeleteStudent`. Render a `<div className="flash-error">` at bottom of component. |
| `frontend/src/index.css` | Style `.flash-error`: fixed to bottom of `.student-list` (position sticky), `--error-red` bg, white text, fade in/out via `AnimatePresence`. |

## 3. Student fetch error indicator

| File | Change |
|------|--------|
| `frontend/src/components/StudentList.tsx` | In `toggleExpand` catch, instead of setting empty array, set a sentinel (e.g. store failed class IDs in a `Set<number>` state `failedClassIds`). When rendering expanded content for a failed class, show a small inline error with a Retry button instead of an empty `<ul>`. |

## 4. Defensive JSON parsing in delete API functions

| File | Change |
|------|--------|
| `frontend/src/api.ts` | In `deleteClass` and `deleteStudent`, change `await resp.json()` to `await resp.json().catch(() => ({}))` so a non-JSON error response (e.g. proxy 502) produces a usable fallback. |

## 5. Tests for AddClassForm and AddStudentForm

| File | Action |
|------|--------|
| `frontend/src/components/__tests__/AddClassForm.test.tsx` | NEW — Tests: renders input + buttons; submit calls `createClass` and fires `onCreated`; shows error on API failure; Esc calls `onCancel`; disables submit when empty. |
| `frontend/src/components/__tests__/AddStudentForm.test.tsx` | NEW — Tests: renders input; submit calls `createStudent` and fires `onCreated`; clears input on success; shows error on failure; disables submit when empty. |

Mock `../../api` module (same pattern as `StudentList.test.tsx`).

## 6. Minor cleanup

| File | Change |
|------|--------|
| `frontend/src/components/StudentList.tsx` | Remove trailing blank line at EOF. |
| `frontend/src/index.css` | Move orphaned `.toolbar-link` rule out of the `/* --- Student list --- */` section into the `/* --- App nav --- */` section where it belongs. |

## Open questions

None.
