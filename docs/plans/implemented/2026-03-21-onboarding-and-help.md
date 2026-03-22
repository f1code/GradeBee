# Onboarding & In-App Help

## Goal

Give new visitors a clear understanding of what GradeBee does before signing in, and give signed-in users a way to learn the workflow at any time.

---

## 1. Pre-sign-in: Expanded sign-in card

**File:** `frontend/src/App.tsx` — the `<Show when="signed-out">` block.

Replace the current minimal copy with a richer card. The card stays centered, keeps the honeycomb backdrop, and grows slightly wider (`max-width: 480px`).

### Content

```
Welcome to GradeBee

Record verbal feedback about your students and GradeBee turns it
into polished, structured notes and report cards — saved straight
to your Google Drive.

  🎤  Record or upload audio of your observations
  🗂️  Notes are created automatically for each student
  📄  Generate report cards that match your writing style

Sign in with Google
```

### Implementation

- In `App.tsx`, expand the sign-in card JSX: add a `<p>` tagline and a `<ul className="feature-list">` with three `<li>` items (emoji + text).
- In `index.css`, bump `.sign-in-card` `max-width` to `480px`. Add `.feature-list` styles: left-aligned, no bullet, generous line-height, `--ink-muted` color, small top/bottom margin. Each `li` gets a leading emoji treated as an inline icon (or use a `::before` with content).

---

## 2. Post-sign-in: "How it works" guide

### Approach — combination of:

**A. Header help link** that opens a modal/overlay walkthrough.
**B. Contextual help hints** on the Notes and Reports tabs.

### A. "How it works" modal

**Trigger:** A `?` or "How it works" pill button in the header (next to the `<UserButton />`), always accessible.

**First-visit auto-show:** On first sign-in (no `gradebee:seenGuide` key in `localStorage`), the modal opens automatically. After dismissal, the key is set. Users can reopen from the header any time.

**Content — 4 steps displayed as a vertical stepper inside a centered modal card:**

| # | Heading | Description |
|---|---------|-------------|
| 1 | **Set up your class list** | Create a Google Sheets spreadsheet with your classes and student names. GradeBee reads it to match recordings to students. |
| 2 | **Record your observations** | Upload or record audio of your verbal feedback. You can also import audio files already in your Drive. |
| 3 | **Review & edit notes** | GradeBee transcribes the audio and creates a structured note for each student mentioned. Review, tweak, and save — notes are stored as Google Docs. |
| 4 | **Generate report cards** | When it's report time, select a date range and students. GradeBee aggregates all notes into a report card that matches your style. Upload example reports so it learns your voice. |

**Design:**
- Modal overlay with semi-transparent `--ink` backdrop.
- Card uses `.card` pattern (chalk bg, warm shadow, 12px radius).
- Each step: honey-colored circled number, Fraunces heading, body text.
- "Got it" primary button at bottom to dismiss.
- Close × in top-right corner.

**Files:**
- New component: `frontend/src/components/HowItWorks.tsx`
- Styles added to `frontend/src/index.css` (`.how-it-works-overlay`, `.how-it-works-card`, `.guide-step`, etc.)
- `App.tsx`: add state `showGuide`, header button, render `<HowItWorks>` when open. `useEffect` to auto-show on first visit.

### B. Contextual help hints

Small, dismissible info banners at the top of each tab for first-time users.

- **Notes tab:** "Record or upload audio, then review the notes GradeBee creates for each student."
- **Reports tab:** "Select students and a date range to generate report cards from your accumulated notes."

Each banner has a small × to dismiss; dismissal stored in `localStorage` per banner (`gradebee:hint:notes`, `gradebee:hint:reports`).

**Design:** Styled as a subtle `--honey-light` background strip with `--ink-muted` text and a `--honey` left border (like an info callout). Animated entry with `motion`.

**Files:**
- New component: `frontend/src/components/HintBanner.tsx` — generic, takes `storageKey`, `children`, renders if not dismissed.
- Used in `SignedInContent` in `App.tsx`, placed above `<StudentList>` / `<ReportGeneration>`.
- Styles in `index.css`.

---

## Summary of file changes

| File | Change |
|------|--------|
| `frontend/src/App.tsx` | Expand sign-in card copy; add guide state + header button; render `<HowItWorks>` and `<HintBanner>` components; auto-show guide on first visit |
| `frontend/src/components/HowItWorks.tsx` | **New** — modal walkthrough component |
| `frontend/src/components/HintBanner.tsx` | **New** — dismissible contextual hint banner |
| `frontend/src/index.css` | `.feature-list`, `.how-it-works-*`, `.hint-banner` styles; widen `.sign-in-card` |

---

## Open questions

1. Should the guide modal have "Next/Back" step navigation, or show all 4 steps at once (vertical stepper)? Leaning toward all-at-once — simpler, scannable, no lost context.
2. Should we show the guide after Drive setup completes (instead of on first sign-in)? First sign-in seems better — it explains what's coming *before* setup.
