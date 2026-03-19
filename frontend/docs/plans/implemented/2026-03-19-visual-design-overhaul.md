# Visual Design Overhaul

## Goal
Transform GradeBee from generic system-ui styling into a distinctive, warm, education-themed interface that feels handcrafted and inviting — like a teacher's favorite notebook.

## Aesthetic Direction: "Warm Classroom"
Organic, slightly textured, warm palette. Think: kraft paper meets modern UI. Friendly but professional — not childish. The bee theme is a gift; lean into it with honeycomb geometry and amber/gold accents.

Light theme only. Single-column centered layout.

## Deliverables

This plan produces two things:
1. The code changes listed below
2. A `frontend/DESIGN.md` file documenting the design system (colors, typography, component patterns, dos/don'ts) so future work stays consistent. Referenced from `AGENTS.md`.

## Proposed Changes

### 1. Typography
- **Display/headings**: [Fraunces](https://fonts.google.com/specimen/Fraunces) — a soft-serif variable font with optical sizing. Warm, distinctive, slightly playful.
- **Body**: [Source Sans 3](https://fonts.google.com/specimen/Source+Sans+3) — clean and highly readable, pairs well.
- File: `index.css` (root font declarations), Google Fonts link in `index.html`

### 2. Color System (CSS custom properties in `index.css`)
| Token | Value | Use |
|---|---|---|
| `--honey` | `#E8A317` | Primary accent, buttons, links |
| `--honey-light` | `#FFF3D4` | Hover backgrounds, highlights |
| `--comb` | `#F5E6C8` | Card backgrounds, drop zone |
| `--ink` | `#2C1810` | Primary text |
| `--ink-muted` | `#7A6B5D` | Secondary text, counts |
| `--parchment` | `#FBF7F0` | Page background |
| `--chalk` | `#FFFFFF` | Card surfaces |
| `--error-red` | `#C53030` | Errors |
| `--success-green` | `#38A169` | Success states |

### 3. Background & Texture
- Subtle noise/grain overlay on `body` using a CSS pseudo-element or tiny repeating SVG — gives a paper-like feel.
- File: `index.css`

### 4. Layout & Spacing
- Increase max-width to `860px`, add more generous vertical rhythm.
- Cards/sections get `background: var(--chalk)`, `border-radius: 12px`, subtle `box-shadow` (warm-toned, not grey).
- File: `index.css`

### 5. Header
- Logo: "GradeBee" with a small inline SVG bee/honeycomb icon designed for the project.
- Bottom border → replace with a decorative honeycomb-pattern divider (thin SVG or CSS gradient).
- File: `App.tsx`, `index.css`

### 6. Buttons
- `background: var(--honey)`, `color: var(--ink)` (dark-on-gold is more distinctive than white-on-indigo).
- Slightly rounded (`border-radius: 8px`), subtle hover: darken + lift (`translateY(-1px)` + shadow increase).
- Transition: `all 0.15s ease`.
- File: `index.css`

### 7. Student List
- Class groups become cards with `var(--chalk)` background.
- Student rows: remove border-bottom, use alternating subtle background (`var(--comb)` at 30% opacity on even rows).
- Class name headings get a small honeycomb bullet or hexagon marker.
- Toolbar links styled as pill-shaped secondary buttons.
- File: `index.css`, minor markup tweaks in `StudentList.tsx`

### 8. Audio Upload Drop Zone
- Dashed border using `var(--honey)` color, `border-radius: 12px`.
- Background: `var(--comb)` with a centered mic/waveform icon (CSS-only or inline SVG).
- Drag-over state: border goes solid, background pulses to `var(--honey-light)`.
- Progress states: add a simple CSS honeycomb-spinner animation.
- Transcript textarea: styled with matching border-radius, warm border color.
- File: `index.css`, small additions to `AudioUpload.tsx` for icon

### 9. Sign-In Page
- Center vertically. Add a large decorative honeycomb pattern (CSS `clip-path` hexagons or SVG) as a background accent behind the card.
- "Welcome to GradeBee" in Fraunces at a larger size.
- File: `index.css`, `App.tsx`

### 10. Info/Empty States
- `.info-box` gets an illustration-style empty state (CSS-only hexagon pattern or a simple bee SVG).
- File: `index.css`

### 11. Micro-interactions
- Add `motion` library (`npm i motion`) for page-load staggered reveals on student list items and card entrances.
- Hover states on student rows: slight indent + honey-light background.
- File: `StudentList.tsx`, `AudioUpload.tsx`, `DriveSetup.tsx`

### 12. Drive Setup Success
- Links styled as cards with icons (folder icon, spreadsheet icon) rather than plain underlined text.
- File: `DriveSetup.tsx`, `index.css`

## Files Touched
| File | Change type |
|---|---|
| `index.html` | Add Google Fonts link |
| `src/index.css` | Major rewrite — variables, typography, all component styles |
| `src/App.tsx` | Logo/header markup, sign-in layout |
| `src/components/StudentList.tsx` | Minor markup for card wrappers, motion animations |
| `src/components/AudioUpload.tsx` | Drop zone icon, spinner, motion |
| `src/components/DriveSetup.tsx` | Success state card layout |
| `package.json` | Add `motion` dependency |
| `DESIGN.md` | New — design system reference |
| `../AGENTS.md` | Add reference to `frontend/DESIGN.md` |
