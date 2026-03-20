# Mobile Responsive Adaptation

## Goal

Make GradeBee usable on mobile devices, with the **notes flow** (student list, audio upload, note confirmation) as the priority. Report card section gets basic tablet-friendly treatment only.

## Current State

- Viewport meta tag ✅ already present
- Only 3 ad-hoc `@media` queries (640px, 480px)
- No breakpoint system, no touch target sizing, no mobile nav consideration
- DESIGN.md has zero mention of responsive strategy

## Proposed Changes

### 1. DESIGN.md — Add Responsive Section
**File:** `frontend/DESIGN.md`

Add a "Responsive" section defining:
- Breakpoints: `--bp-sm: 480px`, `--bp-md: 640px`, `--bp-lg: 860px`
- Touch targets: minimum 44×44px for all interactive elements
- Mobile-first column stacking strategy

### 2. Header — Mobile-Friendly
**File:** `frontend/src/index.css` (header styles)

- Logo + UserButton already flexbox — just ensure they don't overflow
- Shrink logo text slightly at `≤480px`

### 3. App Nav Tabs — Full-Width on Mobile
**File:** `frontend/src/index.css` (`.app-nav`)

- At `≤640px`: `width: 100%` instead of `fit-content`, equal-width tab buttons
- Ensure 44px min-height on tab buttons

### 4. Student List — Collapsible on Mobile
**Files:** `frontend/src/components/StudentList.tsx`, `frontend/src/index.css`

On mobile, the student list pushes the upload zone below the fold. Make it collapsible:
- Add a `collapsed`/`expanded` toggle state (default **collapsed** on mobile, expanded on desktop)
- Collapsed view: show a single summary line, e.g. "3 classes · 24 students" with a chevron to expand
- Use a `useMediaQuery` hook to control default collapsed state
- Animate expand/collapse with `motion` layout animation (consistent with app's existing motion language)
- Toolbar: stack vertically at `≤480px` (Edit in Sheets + Refresh)
- Class group cards: reduce padding, smaller font for student names
- Student `<li>` items: add padding for touch-friendly tap targets

### 5. Audio Upload — Mobile-Native UX
**Files:** `frontend/src/components/AudioUpload.tsx`, `frontend/src/index.css`

Drag-and-drop is useless on mobile. Rethink the upload zone for touch:
- Use the `useMediaQuery` hook (shared with §4) to conditionally render different markup:
  - **Mobile (`≤640px`):** render two prominent, equal-width action buttons stacked vertically:
    - **"Choose Audio File"** (primary style, triggers file picker)
    - **"Add from Drive"** (secondary style)
    - Accepted-formats hint text below
  - **Desktop (`>640px`):** render the current drop zone + Drive button layout unchanged
- Progress/spinner states: tighten padding on mobile

### 6. Note Confirmation — Critical Path
**File:** `frontend/src/index.css` (`.note-confirmation`, `.note-student-card`, `.note-actions`)

This is the most complex mobile view. Changes:
- `.note-student-header`: already has `flex-wrap` ✅, but at `≤480px` stack name above actions vertically (already in existing query, verify it works well)
- `.note-student-actions`: ensure confidence badge + remove button don't get cramped
- `.note-candidates-list`: already `flex-wrap` ✅ — verify buttons have enough tap target
- `.note-candidate-btn`: increase padding on mobile for touch (min 44px height)
- `.note-summary-input`: ensure textarea is comfortable on mobile (min 4 rows?)
- `.note-meta-row`: already stacks at 480px ✅ — date input should be full-width
- `.note-actions`: stack save/cancel buttons vertically at `≤480px`, full-width, save on top
- `.note-actions`: on mobile, make the save button **sticky at the bottom of the viewport** so users don't have to scroll back up after editing the last student. Use a fixed/sticky footer bar with `padding-bottom: env(safe-area-inset-bottom)` for iPhone home indicator clearance.
- `.note-success .note-doc-link`: ensure 44px min-height tap targets

### 7. Buttons & Inputs — Global Touch Targets
**File:** `frontend/src/index.css` (button base styles, input styles)

- Add `min-height: 44px` to primary and secondary buttons
- Ensure `padding` is at least `0.6rem 1rem`
- Add `-webkit-text-size-adjust: 100%` to `:root`
- At `≤640px`: bump all form inputs and textareas to `font-size: 1rem` (16px) to prevent iOS auto-zoom on focus. Current `.note-summary-input` and `.note-meta-field input` are 0.9rem (~14.4px) which triggers zoom.

### 8. Safe Area Insets
**File:** `frontend/src/index.css`

- Add `padding-bottom: env(safe-area-inset-bottom)` to `.app` base padding on mobile
- Apply same to the sticky save bar (§6) so iPhone home indicators don't clip content

### 9. Report Section — Light Touch Only
**File:** `frontend/src/index.css` (report-related styles)

- Keep existing 480px query for `.report-period` and `.report-student-list`
- No additional work — this is tablet/desktop focused

## Open Questions

None — all integrated.
