# GradeBee Design System

**Aesthetic:** "Warm Classroom" — organic, slightly textured, warm palette. Kraft paper meets modern UI. Friendly but professional. Light theme only.

## Colors (CSS custom properties)

| Token | Value | Use |
|---|---|---|
| `--honey` | `#E8A317` | Primary accent, buttons, links |
| `--honey-dark` | `#C4880F` | Hover/pressed states |
| `--honey-light` | `#FFF3D4` | Hover backgrounds, highlights |
| `--comb` | `#F5E6C8` | Card backgrounds, drop zone, borders |
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

### Level and Schedule fields
The class editor (`AddClassForm`, `StudentList`) exposes two fields with distinct purposes — surface this distinction in helper text:
- **Level** (required): the class identifier. It also tags report examples for style matching, so reports for a class reuse examples sharing its level.
- **Schedule** (optional, e.g. "Period 1"): purely organizational — groups classes by schedule slot. Shown as `Level — Schedule`. It has no effect on report generation.

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

### Drop Zone
- Dashed `--honey` border, `--comb` background, `12px` radius.
- Drag-over: solid border + `--honey-light` bg + glow ring.

### Empty/Info States
- `.info-box`: centered card with subtle hex pattern overlay.

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

### Selectable Chips (toggle pills)

For picking from a small set of options (e.g. assigning classes to an example), use pill-shaped toggle chips instead of bare checkboxes. The native checkbox is visually hidden; the `<label>` is the control.

- Unselected: `--chalk` bg, `1px` `--comb` hairline border, pill radius (`999px`), with a light hollow `--comb` check circle.
- Selected (`.is-selected`): pale `--honey-light` bg, `--honey` border, bold label, `--honey`-filled check circle with `--ink` tick — noticeable but not saturated.
- Hover lifts the border to `--honey`; keyboard focus (`:has(input:focus-visible)`) shows an offset dashed `--honey-dark` outline — distinct from the solid selected fill, and suppressed on mouse click.
- Min height `36px` for touch.
- CSS: `.level-names-select` (wrap container), `.level-names-option` / `.level-names-option.is-selected`.

### Guided Action Panel

When a multi-step flow needs the user to make a required choice (e.g. "which class is this example for?" after picking files), surface it as a distinct honey-tinted panel rather than inline text:

- `--chalk` bg, soft `--comb` border with a `3px` `--honey` left accent stripe, `12px` radius, `--shadow-sm`. Light overall — the accent stripe carries the "don't miss this" weight.
- Lead with a posed question in the display font (`.upload-classnames-title`), a one-line `--ink-muted` rationale (`.upload-classnames-help`), then the chosen file(s) as `--parchment` rows.
- Muted uppercase step label introduces the choice; an `--ink-muted` hint line appears while the requirement is unmet.
- Pattern classes: `.upload-classnames-panel`, `.upload-classnames-title/help/files/file/steplabel/hint/actions`.

### Selection-Aware Lists

When a parent view has an active selection (e.g. students from a class), list items that match the selection are highlighted with a `--matching` modifier; items that don't match are dimmed with a `--dimmed` modifier. When nothing is selected, no modifier is applied and all items render at full opacity.

- Matching: `outline: 2px solid var(--honey)` at `border-radius: 8px` on the wrapper, drawn with `outline-offset: -2px` (inset) so it isn't clipped by the collapsible container's `overflow: hidden`.
- Dimmed: `opacity: 0.45` on the wrapper.
- CSS modifier classes: `<wrapper>--matching`, `<wrapper>--dimmed`.
- A summary line (`.example-selection-summary`) uses `var(--honey-dark)` at `0.82rem` to show e.g. *N example(s) will guide these reports.*

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
- **Audio upload**: drop zone replaced with prominent stacked action buttons on mobile.
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
| `modals.css` | How It Works modal, student-detail modal shell |
| `roster-upload.css` | Student list, class group, audio upload, job status, transcript review |
| `reports.css` | Report examples, generation, viewer, history |
| `student-detail-notes.css` | Student detail expansion + tabs, student aliases, note editor |
| `feedback-privacy.css` | Feedback FAB + popover, privacy page |

### Responsive rules
Each file owns its own `@media` blocks, placed after the base rules they override. There is no global responsive file.

### Flat-button reset list
`controls.css` contains a selector list that strips button chrome from elements across features (`.toolbar-link`, `.student-detail-tab`, `.report-examples-toggle`, `.how-it-works-trigger`, etc.). When you add a new flat-button-style element anywhere, add its selector to that list in `controls.css`. It is a known cross-file coupling, not a bug.

### Known cascade quirk
`.report-instructions textarea` has a `font-size: 1rem` rule inside the `@media (max-width: 640px)` block in `controls.css` (inherited from the original global responsive block). This rule is shadowed on mobile by the base `.report-instructions textarea { font-size: 0.95rem }` in `reports.css`, which appears later in the import order. The `1rem` rule is therefore dead on mobile. It is preserved intentionally to keep the cascade identical to the original. Do not remove without auditing `reports.css`.
