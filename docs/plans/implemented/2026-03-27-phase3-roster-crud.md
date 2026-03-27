# Phase 3: Frontend — Roster CRUD

**Goal:** Teachers manage classes and students entirely in-app (no Google Sheets). Remove Drive setup flow.

## Files Changed

| File | Action |
|------|--------|
| `frontend/src/api.ts` | Add class & student CRUD functions |
| `frontend/src/components/StudentList.tsx` | Rewrite with inline CRUD |
| `frontend/src/components/AddClassForm.tsx` | NEW |
| `frontend/src/components/AddStudentForm.tsx` | NEW |
| `frontend/src/components/DriveSetup.tsx` | DELETE |
| `frontend/src/App.tsx` | Remove setup flow |

---

## api.ts — New Functions

Keep: `uploadAudio`, `getGoogleToken`, `importFromDrive`, all report/job functions.

Remove: nothing (no setup endpoint call exists here; it's inline in App.tsx).

### New types

```
ClassItem      { id: number; name: string; studentCount: number }
StudentItem    { id: number; name: string; classId: number }
ClassWithStudents { id: number; name: string; students: StudentItem[] }
```

### New functions

| Function | Method | Endpoint | Body | Returns |
|----------|--------|----------|------|---------|
| `listClasses` | GET | `/classes` | — | `{ classes: ClassItem[] }` |
| `createClass` | POST | `/classes` | `{ name }` | `ClassItem` |
| `renameClass` | PUT | `/classes/:id` | `{ name }` | `ClassItem` |
| `deleteClass` | DELETE | `/classes/:id` | — | `void` |
| `listStudents` | GET | `/classes/:classId/students` | — | `{ students: StudentItem[] }` |
| `createStudent` | POST | `/classes/:classId/students` | `{ name }` | `StudentItem` |
| `renameStudent` | PUT | `/students/:id` | `{ name }` | `StudentItem` |
| `deleteStudent` | DELETE | `/students/:id` | — | `void` |

All functions take `getToken` as last param (same pattern as existing functions). All use `Authorization: Bearer ${token}` header.

---

## App.tsx — Remove Setup Flow

### Remove
- `setupDone` state and `setSetupDone` prop threading
- `useEffect` that calls `GET /setup` to check setup status
- `DriveSetup` import and conditional render
- `onSetupRequired` prop passed to `StudentList`

### Change
- `SignedInContent` no longer gates on `setupDone`. After sign-in, immediately show the nav tabs + content (notes/reports).
- Remove the loading spinner that waits for setup check. The `StudentList` component handles its own loading state when fetching classes.
- Keep everything else: nav tabs, `HintBanner`, `JobStatus`, `AudioUpload`, `ReportGeneration`, `HowItWorks`.

---

## StudentList.tsx — Rewrite

### Data Model

Replace `StudentsResponse` (with `spreadsheetUrl`) with locally-fetched class/student data.

**State:**
- `classes: ClassItem[]` — fetched from `GET /classes`
- `expandedStudents: Map<number, StudentItem[]>` — lazily fetched per class from `GET /classes/:id/students`
- `status: 'loading' | 'error' | 'ready'`
- `editingClassId: number | null` — class currently being renamed inline
- `editingStudentId: number | null` — student currently being renamed inline
- `deletingId: { type: 'class' | 'student'; id: number } | null` — confirmation modal target

**No props** (remove `onSetupRequired`).

### Layout

Top-level `<div className="student-list">` card container.

1. **Header row:** "Your Classes" heading (Fraunces, `--ink`) + "Add Class" button (primary `--honey` btn).
2. **Class list:** Each class is a card (`--chalk` bg, `border-radius: 12px`, warm shadow). Contains:
   - Class name as `<h3>` with `HexBullet` icon (keep existing SVG).
   - Student count badge (`--ink-muted`).
   - Expand/collapse chevron (reuse existing `ChevronIcon`).
   - Inline action icons: rename (pencil), delete (trash) — appear on hover (desktop) or always visible (mobile).
3. **Expanded student list** inside each class card:
   - `<ul>` of student names.
   - Each `<li>` has hover actions: rename, delete.
   - "Add Student" row at bottom (renders `AddStudentForm` inline).

### Interactions

| Action | UI | API Call | Optimistic? |
|--------|----|----------|-------------|
| Expand class | Click class card / chevron | `listStudents(classId)` (cached after first fetch) | No — show honeycomb spinner in card |
| Add class | Click "Add Class" → renders `AddClassForm` above class list | `createClass` | No — disable submit, show spinner, append on success |
| Rename class | Click pencil → `<h3>` becomes `<input>` pre-filled with name. Enter/blur saves, Esc cancels. | `renameClass` | Yes — update local state, revert on error |
| Delete class | Click trash → confirmation dialog: "Delete {name} and all its students?" with Cancel + Delete buttons | `deleteClass` | No — remove from list on success |
| Add student | Click "+ Add student" inside expanded class → shows `AddStudentForm` | `createStudent` | No — append to list on success |
| Rename student | Click pencil → `<li>` text becomes `<input>`. Enter/blur saves, Esc cancels. | `renameStudent` | Yes — revert on error |
| Delete student | Click trash → confirmation: "Delete {name}?" | `deleteStudent` | No — remove on success |

### Design System Compliance

- Cards: `--chalk` bg, `12px` radius, warm `--shadow-md`.
- Buttons: primary uses `--honey` bg + `--ink` text. Delete buttons use `--error-red` text, secondary style.
- Motion: class cards stagger in with `containerVariants` / `cardVariants` (reuse existing). New classes animate in with `motion.div` fade+slide. Deleted items use `AnimatePresence` exit animation (fade + height collapse).
- Empty state: when no classes exist, show `.info-box` card: "No classes yet — add your first class to get started." with the `AddClassForm` embedded.
- Mobile (≤640px): collapse toggle stays (summary: "N classes · M students"). Action icons always visible (no hover). Touch targets ≥44×44px.

### Confirmation Dialog

Inline within the card (not a modal). Replaces the item row with a red-tinted bar: message + Cancel (secondary btn) + Delete (`--error-red` bg). Animated with `AnimatePresence`.

---

## AddClassForm.tsx — New Component

**Props:**
- `onCreated: (cls: ClassItem) => void` — callback after successful creation
- `onCancel: () => void` — callback to hide the form

**State:**
- `name: string`
- `submitting: boolean`
- `error: string | null`

**UI:**
- Single-row form inside a card: text `<input>` (placeholder "Class name") + "Add" primary button + "Cancel" secondary button.
- Input styled: `--chalk` bg, `--comb` border, `8px` radius, `font-family: var(--font-body)`, `1rem` size.
- On submit: call `createClass(name)`. Disable input + button while submitting. On success call `onCreated`. On error (e.g. duplicate name → 409) show `--error-red` message below input.
- Auto-focus input on mount.
- Enter key submits. Esc key calls `onCancel`.

**Motion:** `motion.div` with fade+slide-down entry, `AnimatePresence` for exit.

---

## AddStudentForm.tsx — New Component

**Props:**
- `classId: number`
- `onCreated: (student: StudentItem) => void`

**State:**
- `name: string`
- `submitting: boolean`
- `error: string | null`

**UI:**
- Inline row at the bottom of the student `<ul>`: text input (placeholder "Student name") + "Add" small primary button.
- Always visible when class is expanded (no toggle needed).
- On submit: call `createStudent(classId, name)`. Clear input on success, call `onCreated`. Show error if duplicate (409).
- Enter key submits. Input stays focused after success for rapid entry.
- Input size: `font-size: 1rem` at ≤640px (prevent iOS zoom).

**Motion:** None needed (it's always present in expanded view).

---

## DriveSetup.tsx — Delete

Remove file entirely. No references will remain after App.tsx changes.

---

## Open Questions

1. ~~Class reordering~~ — **No position column.** Classes and students are sorted alphabetically.
2. ~~Bulk import~~ — **Deferred.** Add paste-from-spreadsheet flow later if requested.
3. ~~Student move between classes~~ — **Deferred.** The `PUT /students/:id` endpoint accepts `classId` to support this later. For now, rename and delete cover common cases.
