# Button Default Styling Refactor

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Make base `<button>` look like a primary button by default, so forgetting a class never produces an invisible button. Remove `.btn-primary` class entirely.

**Architecture:** Move `.btn-primary` styles (honey bg, shadow, hover/active 3D effects) into the base `button` rule. Remove all `btn-primary` references from TSX and CSS. Ensure all flat/icon/link buttons explicitly reset background, box-shadow, and hover/active transforms.

**Tech Stack:** CSS, React (TSX)

---

### Task 1: Update base `button` styles in CSS

**Files:**
- Modify: `frontend/src/index.css`

**Step 1: Edit the base button rule and shared hover/active**

In `index.css`, change the `button` rule (~line 160) to add honey background and shadow:

```css
button {
  font-family: var(--font-body);
  font-weight: 600;
  font-size: 0.95rem;
  background: var(--honey);
  color: var(--ink);
  border: none;
  padding: 0.5rem 1.4rem;
  min-height: 44px;
  box-sizing: border-box;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: all 0.15s ease;
  box-shadow: var(--shadow-sm);
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}
```

Change the shared hover/active rules (~line 186) to apply to `button` instead of `.btn-primary`:

```css
button:hover,
.btn-secondary:hover,
.btn-danger:hover {
  transform: translateY(-1px);
  box-shadow: var(--shadow-lift);
}

button:active,
.btn-secondary:active,
.btn-danger:active {
  transform: translateY(0);
  box-shadow: var(--shadow-sm);
}
```

Also add a default hover background (from `.btn-primary:hover`):

```css
button:hover {
  background: var(--honey-dark);
}
```

**Step 2: Remove all `.btn-primary` CSS rules**

Delete these blocks from `index.css`:
- `.btn-primary` (~line 2711-2715) — background/color/shadow
- `.btn-primary:hover` (~line 2717-2719) — background override
- `.paste-actions .btn-primary` (~line 804) — width override → change selector to `.paste-actions button`

**Step 3: Build to verify CSS compiles**

Run: `cd frontend && npm run build`
Expected: build succeeds (TSX still references btn-primary but that's just an unused class)

**Step 4: Commit**

```bash
git add frontend/src/index.css
git commit -m "refactor: make base button primary-styled by default"
```

---

### Task 2: Fix flat button resets in CSS

**Files:**
- Modify: `frontend/src/index.css`

These flat/icon buttons need explicit resets so they don't inherit the honey background and 3D effects. Add missing properties:

**Step 1: Add resets to flat button classes**

`.text-link` — add `box-shadow: none;`

`.toolbar-link` — add `background: none; box-shadow: none;`

`.job-dismiss-btn` — add `box-shadow: none;`

`.student-detail-back` (in the `@media (max-width: 640px)` block where it becomes `display: flex`) — add `box-shadow: none;` (it already has `background: var(--chalk)`)

Also for ALL flat buttons, disable the base hover transform. Add a shared reset rule:

```css
/* Flat buttons: disable 3D hover/active effects */
.text-link:hover,
.text-link:active,
.icon-btn:hover,
.icon-btn:active,
.toolbar-link:hover,
.toolbar-link:active,
.student-detail-tab:hover,
.student-detail-tab:active,
.student-list-collapse-toggle:hover,
.student-list-collapse-toggle:active,
.report-examples-toggle:hover,
.report-examples-toggle:active,
.how-it-works-trigger:hover,
.how-it-works-trigger:active,
.how-it-works-close:hover,
.how-it-works-close:active,
.student-modal-close:hover,
.student-modal-close:active,
.hint-banner-close:hover,
.hint-banner-close:active,
.job-note-link:hover,
.job-note-link:active,
.job-dismiss-btn:hover,
.job-dismiss-btn:active,
.note-show-toggle:hover,
.note-show-toggle:active,
.student-detail-back:hover,
.student-detail-back:active,
.guide-dismiss-btn:hover,
.guide-dismiss-btn:active {
  transform: none;
}
```

Note: Many of these already have their own `:hover` rules with specific effects — that's fine, `transform: none` just prevents the base `translateY(-1px)` from leaking through. If their existing `:hover` already sets `transform: none` this is harmless duplication. Check `.guide-dismiss-btn` and `.mobile-upload-btn` — `guide-dismiss-btn` has no bg reset and should look like a primary button (it IS a primary action), so do NOT include it in the flat reset. Same for `mobile-upload-btn` — it's a primary upload action, keep it styled.

**Step 2: Build and visually verify**

Run: `cd frontend && npm run build`
Expected: succeeds

**Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "refactor: add explicit resets for flat button variants"
```

---

### Task 3: Remove `btn-primary` from all TSX files

**Files:**
- Modify: `frontend/src/App.tsx` — remove `btn-primary` from sign-in button
- Modify: `frontend/src/components/ReportGeneration.tsx`
- Modify: `frontend/src/components/ReportViewer.tsx` (2 occurrences)
- Modify: `frontend/src/components/NoteEditor.tsx`
- Modify: `frontend/src/components/AudioUpload.tsx`
- Modify: `frontend/src/components/StudentList.tsx` (2 occurrences)
- Modify: `frontend/src/components/ReportExamples.tsx`
- Modify: `frontend/src/components/StudentDetail.tsx`
- Modify: `frontend/src/components/AddStudentForm.tsx`
- Modify: `frontend/src/components/AddClassForm.tsx`

**Step 1: Remove `btn-primary` class from all buttons**

For each file, remove `btn-primary` from className strings. Examples:
- `className="btn-primary btn-sm"` → `className="btn-sm"`
- `className="btn-primary report-generate-btn"` → `className="report-generate-btn"`
- `className="btn-primary sign-in-btn"` → `className="sign-in-btn"`
- `className="btn-primary"` → remove className entirely (or leave empty)
- `` className={`btn-primary btn-sm${saving ? ' btn-loading' : ''}`} `` → `` className={`btn-sm${saving ? ' btn-loading' : ''}`} ``
- `` className={`btn-primary btn-sm report-copy-btn${...}`} `` → `` className={`btn-sm report-copy-btn${...}`} ``

**Step 2: Verify no `btn-primary` references remain**

Run: `cd frontend/src && grep -rn 'btn-primary' --include='*.tsx' --include='*.css'`
Expected: no results

**Step 3: Run tests**

Run: `cd frontend && npm test`
Expected: all tests pass (some tests may reference btn-primary in assertions — check and update)

**Step 4: Run e2e tests**

Run: `cd /path/to/repo && npm run test:e2e`
Expected: all 21 tests pass

**Step 5: Commit**

```bash
git add -A
git commit -m "refactor: remove btn-primary class, base button is now primary-styled"
```

---

### Task 4: Update DESIGN.md

**Files:**
- Modify: `frontend/DESIGN.md`

**Step 1: Update the Buttons section**

Replace the current Buttons section (~line 32-36) with:

```markdown
### Buttons
- Base `<button>` is primary-styled by default: `background: var(--honey)`, `color: var(--ink)`, shadow, 3D hover lift. No class needed.
- Secondary (`.btn-secondary`): white bg with `--comb` border.
- Danger (`.btn-danger`): red bg, white text.
- Small (`.btn-sm`): reduced padding/font.
- Flat variants (`.text-link`, `.icon-btn`, tabs, toggles) explicitly reset background/shadow/transform.
- Do NOT use `.btn-primary` — it doesn't exist. A bare `<button>` is already primary.
- Hover: darken + subtle lift (`translateY(-1px)` + shadow increase).
- `border-radius: 8px`.
```

**Step 2: Commit**

```bash
git add frontend/DESIGN.md
git commit -m "docs: update DESIGN.md with new button conventions"
```

---

### Open Questions

- Should `btn-secondary` hover still override to `var(--honey-dark)` background, or keep its own lighter hover? (Currently it does NOT override bg on hover, just gets the 3D lift — this is correct)
- The `sign-in-btn` class sets width/size but no bg — after this change it inherits honey bg correctly. Verify it still looks right.
