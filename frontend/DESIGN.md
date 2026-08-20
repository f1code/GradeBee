# GradeBee Design System

**Aesthetic:** "Warm Classroom" — organic, slightly textured, warm palette. Kraft paper meets modern UI. Friendly but professional. Light theme only.

## Colors (CSS custom properties)

| Token | Value | Use |
|---|---|---|
| `--honey` | `#E8A317` | Primary accent, buttons, links |
| `--honey-dark` | `#C4880F` | Hover/pressed states |
| `--honey-light` | `#FFF3D4` | Hover backgrounds, highlights |
| `--comb` | `#F5E6C8` | Card backgrounds, recording card, borders |
| `--ink` | `#2C1810` | Primary text |
| `--ink-muted` | `#7A6B5D` | Secondary text, counts, captions |
| `--parchment` | `#FBF7F0` | Page background |
| `--chalk` | `#FFFFFF` | Card surfaces |
| `--error-red` | `#C53030` | Error states |
| `--success-green` | `#38A169` | Success states |

## Typography

- **Display/headings:** [Fraunces](https://fonts.google.com/specimen/Fraunces) — `var(--font-display)`. Soft-serif variable font, warm and distinctive.
- **Body:** [Source Sans 3](https://fonts.google.com/specimen/Source+Sans+3) — `var(--font-body)`. Clean, readable, pairs well with Fraunces.
- All headings use Fraunces at weight 500. Body text at 400.

## Component Patterns

### Cards
- `background: var(--chalk)`, `border-radius: 12px`, warm box-shadow (`--shadow-md`).
- Used for: class groups, setup panels, upload states, sign-in card.

### Level and Time slot fields
The class editor (`AddClassForm`, `StudentList`) exposes two fields with distinct purposes — surface this distinction in helper text:
- **Level** (required): a `<select>` over the Group's Levels (fetched via `listLevels`), never free text — Teachers pick, they don't type. Also drives report style via that Level's Report Instructions. An empty Level list shows an "ask your admin" message instead of the form.
- **Time slot** (optional, e.g. "Period 1", "14:10"): purely organizational free text — groups classes by time slot. Shown as `Level · Time slot`. It has no effect on report generation.

### Buttons
- Base `<button>` is primary-styled by default: `background: var(--honey)`, `color: var(--ink)`, shadow, 3D hover lift. No class needed.
- Secondary (`.btn-secondary`): white bg with `--comb` border.
- Danger (`.btn-danger`): red bg, white text.
- Small (`.btn-sm`): reduced padding/font.
- Flat variants (`.text-link`, `.icon-btn`, tabs, toggles) explicitly reset background/shadow/transform.
- Do NOT use `.btn-primary` — it doesn't exist. A bare `<button>` is already primary.
- Hover: darken + subtle lift (`translateY(-1px)` + shadow increase).
- `border-radius: 8px`.

### Links
- Color: `var(--honey-dark)`. Underline with faded honey color.
- Toolbar links are pill-shaped (`.toolbar-link`) with icon + label.

### Recording card
- Large warm card (`.recording-card`): `--comb` background, `12px` radius, `--shadow-md`. Primary idle action on Add Notes. Recording and review reuse the same card (`.recording-panel`).
- Heading **Record observations live** in Fraunces; supporting copy in `--ink-muted`.
- Prominent primary **Start recording** button (default honey button) with a recording icon and visible text.
- While recording: centered **● Recording** status, large tabular timer, muted size, then **Stop** / **Cancel**. The timer is not in the live region.
- When live recording is unsupported, the card stays and states **Live recording isn't available in this browser.** **Upload audio** becomes the primary button inside the card and is not duplicated below.

### Secondary note-entry actions
- Label **Or add existing notes** (`.existing-notes-label`) sits under the recording card.
- `.secondary-actions` is a centered row of equal `.btn-secondary` actions: **Upload audio**, **Select from Drive**, **Enter text**.
- On mobile (≤640px, or `.secondary-actions--stack`), the row becomes a vertical stack with `min-height: 44px` and full width. The recording card stays first.

### File drop
- There is no boxed idle drop zone and no copy that advertises drag-and-drop, accepted formats, or the 25 MB limit.
- File drops are handled at the Notes tab viewport (window listeners while Add Notes is mounted), not on Reports or Levels.
- While a file drag is over the Notes page, `.notes-drop-overlay` covers the viewport with honey wash (`--honey-light`), a solid `--honey` border, and a glow ring. Copy: **Drop audio to upload**. `pointer-events: none` so it does not steal the drop or flicker on nested targets. Hide it when the drag leaves or the drop completes. Do not show it while recording, reviewing, uploading, or while the Enter text modal is open.

### Notes tab stack
Signed-in Notes tab (`activeTab === 'notes'` in `App.tsx`) is a single column. Do not add a Classes / Students / Record tab; `activeTab` stays `'notes' | 'reports' | 'levels'`.

1. Hint banner (unchanged)
2. Recording / Add Notes (`AudioUpload`) first
3. Job status when any jobs exist (`JobStatus` returns `null` otherwise)
4. Roster (`StudentList`) below — loading, fetch error, and **No Classes Yet** stay in this slot. Class **cards** start collapsed behind the summary toggle at ≤640px; **Your Classes** and **+ Add Class** stay visible. Desktop (`> 640px`) stays expanded.

How It Works onboarding still describes the lifetime workflow (set up classes, then record). That is not the daily screen order.

### Modals
How It Works and student-detail use a dimmed overlay, chalk card, and × close (`modals.css`). Enter text on Add Notes reuses the How It Works classes (`how-it-works-overlay` / `how-it-works-card` / `how-it-works-close`) rather than a new visual language. Overlay click does **not** dismiss Enter text — only × or Escape. `.paste-text-modal-close` is 44×44 to meet the touch-target minimum.

### Empty/Info States
- `.info-box`: centered card with subtle hex pattern overlay. Used for Levels, notes list, report history, and roster empties.
- Roster empty uses the same chrome as the populated list: **Your Classes** + **+ Add Class** sit outside the slot. The slot is either `.info-box` (**No Classes Yet**) or the add-class form — never both, and never wrap the header in `.info-box`.
- Add-class with no Levels uses `.info-box` (**No Levels yet** / ask an Admin), not `.add-class-form`.

### Animations
- Use `motion` library for page-load stagger and state transitions.
- Honeycomb spinner (`.honeycomb-spinner`) for loading states.
- Student list cards stagger in on load.

## Bee Theme Elements

- **Logo:** Inline SVG bee inside hexagon, paired with "GradeBee" in Fraunces.
- **Header divider:** Repeating honeycomb-stripe gradient (not a plain border).
- **Level bullets:** Small filled hexagon SVG (`.hex-bullet`).
- **Background texture:** Subtle SVG noise overlay on body (paper-grain feel).
- **Decorative patterns:** Honeycomb hex grid used sparingly behind sign-in and empty states.

## Do's

- Use warm shadows (`rgba(44, 24, 16, ...)` not grey).
- Use generous vertical rhythm and padding.
- Keep the honey accent dominant — it's the brand color.
- Use motion for page entrances and state transitions.
- Use card-style layouts for grouping related content.

## Don'ts

- Don't use grey/blue tones for accents or shadows.
- Don't use `system-ui` or generic sans-serif. Always use the declared font variables.
- Don't add a dark theme (light-only by design).
- Don't use flat borders where a card shadow works better.
- Don't overuse the bee/honeycomb motifs — they should feel like accents, not wallpaper.

### Levels Admin Screen

A third tab (`🐝 Levels`), visible only when `useAuth().has({ role: 'org:admin' })` — Teachers never see it; the real gate is the backend's `isAdmin(r)` check on write endpoints, this is UX only. No router; `App.tsx` keeps `activeTab: 'notes' | 'reports' | 'levels'` local state, same pattern as the Reports tab.

- List uses the existing `<ItemRow>` pattern (`.hex-bullet` badge, pencil rename action, trash→`.delete-confirm` inline). Expanding a row reveals its Report Instructions editor.
- Rename reuses the `.inline-edit-input` / `.inline-class-edit` pattern from `StudentList`.
- Report Instructions is a `<textarea>` inside `.report-instructions` (same class reports/generation uses — see "Known cascade quirk" below), auto-saved on blur. An empty textarea shows an inline `--honey-dark` hint that reports can't generate for this Level yet.
- Add form reuses `.add-class-form` / `.add-class-input` styling. Duplicate name (case-insensitive) within the Group is caught client-side before submit, surfaced via `<InlineError>`; a 409 from a race is surfaced the same way.
- New classes: `.levels-admin`, `.levels-admin-header`, `.levels-admin-help`, `.levels-admin-list`, `.levels-admin-instructions`, `.levels-admin-instructions-status/saved/empty` — defined in `levels.css`.

### Report Generation — Per-Level Instructions Gate

`ReportGeneration.tsx` shows one collapsed, read-only block per distinct Level
among the currently-selected students — keyed by `levelId`, not per student or
per Class, so 12 students across 3 Levels render 3 blocks. A Level whose
`reportInstructions` is empty or whitespace-only (`.trim() === ''`, matching the
server's `strings.TrimSpace` gate) renders instead as a named, expanded blocker
and disables Generate — the client check is UX only, the server pre-flight is
the real gate.

- Two states of the same block: `.report-level-block` (default) vs
  `.report-level-block-blocker` (unset). `data-testid="level-instructions-block"`
  / `"level-instructions-blocker"` distinguish them in tests.
- Instructions render inside a native `<details>`/`<summary>` — no edit
  affordance on this screen; editing lives on the Levels Admin screen.
- New classes: `.report-levels`, `.report-level-block`,
  `.report-level-block-blocker`, `.report-level-name`,
  `.report-level-blocker-text`, `.report-level-instructions`,
  `.report-level-instructions-text` — defined in `reports.css`.

### Error Patterns

Three variants for communicating errors to users:

#### `<InlineError>` (inline, non-blocking)

Use for errors scoped to a specific form field or panel (alias conflict, add-student duplicate, load failure, etc.).

```tsx
<InlineError title='"Katie"' onDismiss={() => setError(null)}>
  is already used by Katherine in this class.
</InlineError>
```

Props:
- `title?` — bolded key value (user's input verbatim, e.g. `"Katie"`). Appears before children.
- `severity?` — `'error'` (default) | `'warning'` | `'info'`. Controls border/bg color.
- `onDismiss?` — if provided, renders a ✕ dismiss button.
- `children` — explanatory message text.

Severity colors:
- `error`: `--error-red` tinted border + bg
- `warning`: `--honey` / `--honey-dark` tinted
- `info`: `--ink-muted` tinted

**Bold-key convention:** when an error involves a specific value the user typed, put that value verbatim in `title` (quoted). Put the conflicting entity's name in the body text.

#### `.flash-error` (transient sticky toast)

Use for global/navigation-level errors that appear and auto-dismiss or require an action unrelated to a specific field. Rendered as a sticky banner at the bottom of a list/panel. Uses `--error-red` background. Do **not** use for field-level errors — use `<InlineError>` instead.

## Responsive

### Breakpoints
- `480px` (sm) — phone portrait. Stack layouts vertically, larger touch targets.
- `640px` (md) — phone landscape / small tablet. Full-width nav tabs, collapsible student list, mobile upload UX.
- `860px` (lg) — max content width.

### Touch targets
- All interactive elements must be at least **44×44px** on mobile (buttons, links, list items).
- Form inputs must be `font-size: 1rem` (16px) at ≤640px to prevent iOS auto-zoom.

### Strategy
- **Mobile-first column stacking**: flex layouts wrap/stack at narrow widths.
- **Student list**: collapses on mobile (≤640px) with a summary toggle; expanded on desktop.
- **Audio upload**: recording card first; secondary actions in a row on desktop and a 44px stack on mobile.
- **Note confirmation save bar**: sticky at viewport bottom on mobile with safe-area inset padding.
- **Safe area insets**: `env(safe-area-inset-bottom)` applied to sticky bars and app padding for iPhone home indicator clearance.

## Stylesheet organisation

`frontend/src/index.css` is the **only import** (`main.tsx` imports it). It contains nothing but `@import` statements. Do not add rules there.

All rules live under `frontend/src/styles/`:

| File | Contents |
|---|---|
| `tokens.css` | CSS custom properties: colors, shadows, radii, font stacks |
| `base.css` | Paper-grain texture, `body`, global typography (`h1`–`h4`, `p`, `a`) |
| `shell.css` | App chrome only: `.app`, header, honeycomb divider, logo, bee-icon, `app-nav`, header-actions, footer |
| `sign-in.css` | Sign-in page, feature list, consent checkbox |
| `controls.css` | Buttons, `icon-btn`, `item-row`, cards (incl. `info-box`), forms, `inline-edit`, `delete-confirm`, `flash-error`, `hint-banner`, inline error card |
| `modals.css` | How It Works, student-detail, and Enter text modal shells |
| `roster-upload.css` | Student list, class group, audio upload, job status, transcript review |
| `reports.css` | Report generation, viewer, history |
| `student-detail-notes.css` | Student detail expansion + tabs, student aliases, note editor |
| `feedback-privacy.css` | Feedback FAB + popover, privacy page |
| `levels.css` | Levels admin screen (list, add form, Report Instructions editor) |

### Responsive rules
Each file owns its own `@media` blocks, placed after the base rules they override. There is no global responsive file.

### Flat-button reset list
`controls.css` contains a selector list that strips button chrome from elements across features (`.toolbar-link`, `.student-detail-tab`, `.how-it-works-trigger`, etc.). When you add a new flat-button-style element anywhere, add its selector to that list in `controls.css`. It is a known cross-file coupling, not a bug.

### Known cascade quirk
`.report-instructions textarea` has a `font-size: 1rem` rule inside the `@media (max-width: 640px)` block in `controls.css` (inherited from the original global responsive block). This rule is shadowed on mobile by the base `.report-instructions textarea { font-size: 0.95rem }` in `reports.css`, which appears later in the import order. The `1rem` rule is therefore dead on mobile. It is preserved intentionally to keep the cascade identical to the original. Do not remove without auditing `reports.css`.
