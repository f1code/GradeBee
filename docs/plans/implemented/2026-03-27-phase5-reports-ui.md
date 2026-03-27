# Phase 5: Frontend — Reports UI

**Goal:** Replace Google Drive/Docs-based report output with inline HTML rendering, clipboard copy, report history, and regeneration with feedback.

**Estimated effort:** 1–2 days

---

## API Changes (`frontend/src/api.ts`)

### Updated Types

```
ReportResult {
  id: number           // DB id (was docId/docUrl)
  student: string
  class: string
  studentId: number
  html: string         // full HTML content
  startDate: string
  endDate: string
  instructions?: string
  createdAt: string
  skipped: boolean
}

ReportSummary {
  id: number
  startDate: string
  endDate: string
  createdAt: string
}
```

### Updated Functions

| Function | Change |
|----------|--------|
| `generateReports()` | Request now uses `studentIds: number[]` instead of `students: {name, class}[]`. Response returns `ReportResult[]` with `html` field, no `docId`/`docUrl`. |
| `regenerateReport()` | New signature: `POST /reports/:id/regenerate` with `{ feedback: string }`. Returns updated `ReportResult`. |

### New Functions

| Function | Endpoint | Returns |
|----------|----------|---------|
| `listStudentReports(studentId)` | `GET /students/:studentId/reports` | `{ reports: ReportSummary[] }` |
| `getReport(id)` | `GET /reports/:id` | `ReportResult` |
| `deleteReport(id)` | `DELETE /reports/:id` | `void` |

### Removed

- `ReportResult.docId`, `ReportResult.docUrl`, `ReportResult.skipped` fields removed.
- Old `regenerateReport()` signature (was based on docId).

---

## Components

### 1. ReportGeneration.tsx (rewrite)

**What changes:** Results section no longer shows "Open in Docs" links. Instead, each result expands inline to show the HTML report via `ReportViewer`.

**Props:** None (top-level page component).

**State:**
- Same as current: `classes`, `selected`, `startDate`, `endDate`, `instructions`, `generating`, `results`, `error`
- New: `expandedReportId: number | null` — which result is currently expanded

**User interactions:**
- Student selection, date picking, instructions — unchanged
- Generate button — calls updated `generateReports()` with `studentIds`
- Results list: each item shows student name + class. Click to expand/collapse.
- Expanded item renders `<ReportViewer>` inline below the item row

**API calls:** `generateReports()` (updated)

**Design system:**
- Results list items: `.report-result-item` cards (`--chalk` bg, `12px` radius, warm shadow)
- Expanded state: card grows with `motion` layout animation
- Generate button: `.btn-primary` with honeycomb spinner while loading
- Student selector, date pickers, instructions textarea — unchanged from current

**Responsive:** Results stack vertically. Expanded report viewer takes full width. On mobile (≤640px), report viewer is full-bleed within the card.

---

### 2. ReportViewer.tsx (new)

**Purpose:** Renders an HTML report with copy-to-clipboard and action buttons. Used both in ReportGeneration results and ReportHistory.

**Props:**
```
reportId: number
html: string
studentName: string
onRegenerate?: () => void   // triggers regenerate flow
onDelete?: () => void       // triggers delete
```

**State:**
- `copied: boolean` — flash "Copied!" confirmation (auto-resets after 2s)
- `showRegenerateForm: boolean`
- `feedback: string` — textarea content for regeneration
- `regenerating: boolean`

**Rendering the HTML:**
- Sanitize HTML with `dompurify` before rendering
- Use a `<div>` with `dangerouslySetInnerHTML` to render the sanitized HTML
- The HTML from the backend is **self-contained styled HTML** (inline styles, no external CSS) designed for copy/paste into email/Word
- Wrap in a container with `user-select: all` so clicking selects the entire report for easy manual copy
- Container styled with a subtle `--comb` border to visually frame the report content, distinguishing it from the app UI

**Copy to clipboard:**
- "Copy to Clipboard" button (`.btn-primary`) above the report
- Uses `navigator.clipboard.write()` with `text/html` MIME type to preserve formatting when pasting into email/Word
- Fallback: `navigator.clipboard.writeText()` with plain text extraction
- On success: button text changes to "✓ Copied!" with `--success-green` color for 2 seconds

**Regenerate flow:**
- "Regenerate" button (`.btn-secondary`) next to copy button
- Click reveals a `<textarea>` below the report with placeholder "What should be different? e.g. 'Make it shorter', 'Focus more on math skills'"
- "Submit" button (`.btn-primary`) + "Cancel" link
- Calls `POST /reports/:id/regenerate` with `{ feedback }`. On success, `html` prop updates via parent callback (`onRegenerate`). Spinner during request.

**Delete:**
- Small "Delete" link (`.btn-secondary`, `--error-red` text) in the action bar
- Confirm with browser `confirm()` dialog
- Calls `DELETE /reports/:id`, then `onDelete` callback

**Design system:**
- Action bar: flex row with copy + regenerate + delete buttons, gap `0.5rem`
- Report frame: `border: 1px dashed var(--comb)`, `padding: 1.5rem`, `border-radius: 8px`, `background: var(--chalk)`
- Feedback textarea: standard app textarea style, `rows={3}`
- Motion: fade-in for the report content, layout animation for regenerate form expand/collapse

**Responsive:** Report frame is full-width. Action buttons wrap to two rows on mobile. Copy button always first/prominent.

---

### 3. ReportHistory.tsx (new)

**Purpose:** List past reports for a specific student, with expand-to-view.

**Props:**
```
studentId: number
studentName: string
```

**State:**
- `reports: ReportSummary[]`
- `loading: boolean`
- `expandedId: number | null` — which report is expanded
- `expandedReport: ReportResult | null` — full report data (fetched on expand)
- `loadingReport: boolean`

**User interactions:**
- On mount: calls `GET /students/:studentId/reports` → populates list
- Each list item shows: date range (`startDate – endDate`), `createdAt` formatted as relative time ("2 days ago")
- Click item to expand → calls `GET /reports/:id` to fetch full HTML → renders `<ReportViewer>` inline
- Click again to collapse (or click a different item)
- Delete via `ReportViewer`'s delete action → remove from list, collapse

**API calls:**
- `listStudentReports(studentId)` on mount
- `getReport(id)` on expand
- `deleteReport(id)` via ReportViewer callback

**Design system:**
- Section heading: "Past Reports" in Fraunces (`--font-display`)
- List items: card rows (`--chalk` bg), stagger-in with `motion`
- Date range in `--ink`, created-at in `--ink-muted`
- Empty state: `.info-box` — "No reports generated yet for {studentName}."
- Loading: honeycomb spinner

**Responsive:** Full-width list. Expanded report viewer takes full width below the item.

**Placement:** Rendered inside `StudentDetail.tsx` (from Phase 4), below the notes timeline. Tab or section toggle: "Notes" | "Reports".

---

## Integration Points

### StudentDetail.tsx (Phase 4)

Add a tab/section selector at the top: **Notes** | **Reports**. Default to Notes. When Reports is selected, render `<ReportHistory studentId={id} studentName={name} />`.

Tab style: pill-shaped toggle (`.toolbar-link` pattern from design system), honey accent on active tab.

### ReportGeneration.tsx student selector

The student selector currently uses `{name, class}` objects. Update to use `studentId: number` from the Phase 3 roster data. The classes/students data now comes from `GET /classes` (Phase 3 API) which returns IDs.

Selection state changes from `Record<string, Set<string>>` (className → studentNames) to `Set<number>` (studentIds). Class-level toggle adds/removes all student IDs in that class.

---

## Open Questions

1. ~~**Report HTML sanitization**~~ — **Yes**, add `dompurify` as a dep. Sanitize all HTML before `dangerouslySetInnerHTML`.
2. ~~**Report history pagination**~~ — No pagination for MVP. Revisit if needed.
3. ~~**Streaming generation**~~ — Deferred to post-MVP.
