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

### Buttons
- Primary: `background: var(--honey)`, `color: var(--ink)`. Dark-on-gold.
- Secondary (`.btn-secondary`): white bg with `--comb` border.
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
- **Class group bullets:** Small filled hexagon SVG (`.hex-bullet`).
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
