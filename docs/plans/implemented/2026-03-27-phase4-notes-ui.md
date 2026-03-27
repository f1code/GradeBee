# Phase 4: Frontend — Notes UI

## Goal

Teachers can browse, edit, and manually add observation notes per student. Notes are displayed in a date-grouped timeline within a student detail view.

## Navigation: Student List → Student Detail

**Approach: Inline expansion** (not a separate route or slide-out panel).

Clicking a student name in `StudentList` expands an inline `StudentDetail` panel below that student's row. Only one student can be expanded at a time. Clicking again (or clicking another student) collapses it. This keeps context visible (the class/roster stays on screen) and avoids routing complexity.

- On mobile (≤640px): the expansion takes full width and pushes content down; a sticky "← Back to list" bar appears at the top of the expanded area.
- `StudentList` gains state: `expandedStudentId: number | null`.
- Student names become clickable (`cursor: pointer`, honey underline on hover).

## API additions (`api.ts`)

All functions follow the existing pattern: accept `getToken`, call `${apiUrl}/...`, throw on error.

```ts
// Types
interface Note {
  id: number
  studentId: number
  date: string        // YYYY-MM-DD
  summary: string     // markdown
  transcript: string | null
  source: 'auto' | 'manual'
  createdAt: string
  updatedAt: string
}

// Functions
listNotes(studentId: number, getToken): Promise<{ notes: Note[] }>
// GET /students/:studentId/notes — returns notes sorted date desc

getNote(noteId: number, getToken): Promise<Note>
// GET /notes/:id

createNote(studentId: number, data: { date: string; summary: string }, getToken): Promise<Note>
// POST /students/:studentId/notes — source='manual', transcript=null

updateNote(noteId: number, data: { summary: string }, getToken): Promise<Note>
// PUT /notes/:id

deleteNote(noteId: number, getToken): Promise<void>
// DELETE /notes/:id
```

## Components

### 1. `StudentDetail.tsx`

**Props:**
- `studentId: number`
- `studentName: string`
- `className: string`

**State:**
- `notes: Note[]` — fetched on mount via `listNotes`
- `status: 'loading' | 'error' | 'success'`
- `editingNoteId: number | null` — which note is being edited (null = none)
- `addingNote: boolean` — whether the "add note" form is open

**Rendering:**
- Header row: student name (Fraunces h3), class name as muted subtitle, "Add Note" primary button (right-aligned).
- Below header: `<NotesList>` component.
- If `addingNote`: a `<NoteEditor>` appears above the notes list (new note form).
- Card style: `--chalk` bg, 12px radius, warm shadow. Sits inside the class-group expansion area.

**API calls:** `listNotes` on mount. Refreshes list after create/update/delete.

**Design system usage:** Card container, Fraunces heading, `--ink-muted` for subtitle, primary button for "Add Note", `motion` fade-in on mount.

---

### 2. `NotesList.tsx`

**Props:**
- `notes: Note[]`
- `onEdit: (noteId: number) => void`
- `onDelete: (noteId: number) => void`
- `editingNoteId: number | null`
- `onSaveEdit: (noteId: number, summary: string) => Promise<void>`
- `onCancelEdit: () => void`

**State:** None (stateless display component; edit state managed by parent).

**Rendering:**
- Groups notes by `date` (YYYY-MM-DD). Each date group has a date header (formatted nicely, e.g. "March 25, 2026") in Fraunces weight 500, `--ink-muted` color.
- Each note is a card-like row:
  - **Source badge:** Small pill — "Auto" (`--honey-light` bg) or "Manual" (`--comb` bg), `--ink-muted` text, 6px radius.
  - **Summary:** Rendered as plain text with `white-space: pre-wrap` for now. If summaries end up being HTML (TBD), swap to `dangerouslySetInnerHTML` with a sanitizer (e.g. `DOMPurify`). Truncated to ~3 lines with "Show more" toggle.
  - **Transcript toggle** (auto notes only): Collapsed by default. "Show transcript" link expands to show the raw transcript text in a `--parchment` bg block with `--ink-muted` text and monospace-ish styling.
  - **Actions:** Small icon buttons (edit ✏️, delete 🗑️) on hover / always visible on mobile. Delete requires a confirmation step (inline "Are you sure?" with cancel/confirm).
- If `editingNoteId` matches a note, that note's summary is replaced with an inline `<NoteEditor>` in edit mode.
- Empty state: Centered `info-box` — "No notes yet. Add one manually or upload audio to generate notes automatically."

**Design system usage:** Cards for note rows, `--comb` border between date groups, stagger animation on load (motion `containerVariants`/`cardVariants` pattern from StudentList), `--ink-muted` for secondary text, `--honey-dark` for action links.

**Responsive:** On mobile, action buttons are always visible (no hover). Transcript block scrolls horizontally if needed. Touch targets ≥44px.

---

### 3. `NoteEditor.tsx`

**Props:**
- `mode: 'create' | 'edit'`
- `initialSummary?: string` — pre-filled for edit mode
- `initialDate?: string` — pre-filled for edit mode; defaults to today for create
- `onSave: (data: { date: string; summary: string }) => Promise<void>`
- `onCancel: () => void`
- `saving: boolean`

**State:**
- `summary: string` — textarea content
- `date: string` — date input value (YYYY-MM-DD)

**Rendering:**
- Date field: `<input type="date">` — editable in create mode, read-only in edit mode. Styled with `--comb` border, `--chalk` bg, 8px radius.
- Summary field: Plain `<textarea>` (per master plan: textarea for MVP). Auto-grows with content (min 4 rows). Placeholder: "Write your observation..." Styled: `--chalk` bg, `--comb` border, `font-family: var(--font-body)`, 8px radius, 1rem font size (prevents iOS zoom).
- Action bar: "Save" primary button + "Cancel" secondary button. Save is disabled when summary is empty or `saving` is true. Shows honeycomb spinner on save.
- On mobile (≤640px): action bar is sticky at viewport bottom with `env(safe-area-inset-bottom)` padding (per DESIGN.md).

**User interactions:**
- Type in textarea, pick date, click Save → calls `onSave`.
- Click Cancel → calls `onCancel` (if content has changed, show "Discard changes?" confirmation).
- Keyboard: Cmd+Enter to save (desktop).

**Design system usage:** Card styling for the editor container, primary/secondary buttons, `--honey` focus ring on textarea, motion slide-down entrance.

---

## Integration into existing components

### `StudentList.tsx` changes

- Student `<li>` items become clickable. Add `onClick` → set `expandedStudentId`.
- Needs student IDs (currently only has names). Update to use new API response shape with `id` fields (from Phase 3 changes).
- Below each `<li>`, conditionally render `<StudentDetail>` when that student is expanded.
- Add visual indicator on expanded student: honey left border or subtle `--honey-light` bg.
- Chevron icon next to student name (rotates when expanded), reusing the existing `ChevronIcon` component.

### `App.tsx` changes

- No routing changes needed (inline expansion approach).
- No new tabs. Notes tab continues to show StudentList; detail is inline.

## Decisions

- **No optimistic updates** — always wait for server response, simpler error handling.
- **Date is read-only on edit** — `PUT /notes/:id` only updates summary; date is immutable after creation.
- **Summary format TBD** — may be plain text or HTML. Implement as plain text initially; if HTML, add `DOMPurify` sanitization and render via `dangerouslySetInnerHTML`.
