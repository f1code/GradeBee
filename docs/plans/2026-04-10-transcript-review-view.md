# Transcript Review View Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** After voice note processing completes, show the full transcript alongside extracted per-student notes so the teacher can verify extraction quality.

**Architecture:** The `Transcript` field already exists on `VoiceNoteJob` (Go struct) and is pre-populated for the text-paste path. For audio uploads, we need to persist it after Whisper returns. On the frontend, regenerate types to pick up the field, then add an expandable transcript review panel to `DoneJobCard` that shows transcript on the left and student extractions on the right. Lightweight — no new endpoints, no DB changes.

**Tech Stack:** Go backend (pipeline fix), TypeScript/React frontend, tygo codegen

---

### Task 1: Persist transcript on job after audio transcription

**Status:** The `Transcript` field already exists on `VoiceNoteJob`. For text-paste, `job.Transcript` is set before processing. But for audio, the local `transcript` variable is never copied back to the job.

**Files:**
- Modify: `backend/voice_note_process.go`

**Step 1: Set transcript on job after transcription**

In `processVoiceNote`, after the audio transcription branch (the `else` block that calls `transcriber.Transcribe`), set the transcript on the job so it rides along through status updates:

```go
// After: transcript, err = transcriber.Transcribe(...)
job.Transcript = transcript
```

This should go right after the successful `Transcribe` call, before the `--- Step 2: Extract ---` section.

**Step 2: Run tests**

Run: `cd backend && make test`
Expected: PASS

**Step 3: Run lint**

Run: `cd backend && make lint`
Expected: PASS

**Step 4: Commit**

```bash
git add backend/voice_note_process.go
git commit -m "fix: persist transcript on job after audio transcription"
```

---

### Task 2: Regenerate TypeScript types

The Go struct has `Transcript string json:"transcript,omitempty"` but the generated TS types are missing it.

**Files:**
- Modify: `frontend/src/api-types.gen.ts` (auto-generated)

**Step 1: Run tygo**

Run: `cd backend && make generate`

**Step 2: Verify the generated file has the new field**

Check that `VoiceNoteJob` in `frontend/src/api-types.gen.ts` now has:
```typescript
transcript?: string;
```

**Step 3: Commit**

```bash
git add frontend/src/api-types.gen.ts
git commit -m "chore: regenerate types with transcript field"
```

---

### Task 3: Build transcript review panel component

**Files:**
- Create: `frontend/src/components/TranscriptReview.tsx`
- Create: `frontend/src/components/__tests__/TranscriptReview.test.tsx`

**Step 1: Write the test**

```tsx
// frontend/src/components/__tests__/TranscriptReview.test.tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import TranscriptReview from '../TranscriptReview'

describe('TranscriptReview', () => {
  const defaultProps = {
    transcript: 'Today I observed that Emma did great on her math test. Jacob was struggling with reading.',
    noteLinks: [
      { name: 'Emma', noteId: 1, studentId: 10, className: 'Class A' },
      { name: 'Jacob', noteId: 2, studentId: 11, className: 'Class A' },
    ],
  }

  it('renders transcript text', () => {
    render(<TranscriptReview {...defaultProps} />)
    expect(screen.getByText(/Today I observed/)).toBeInTheDocument()
  })

  it('renders student note links', () => {
    render(<TranscriptReview {...defaultProps} />)
    expect(screen.getByText('Emma')).toBeInTheDocument()
    expect(screen.getByText('Jacob')).toBeInTheDocument()
  })

  it('shows class name for each student', () => {
    render(<TranscriptReview {...defaultProps} />)
    expect(screen.getAllByText('Class A')).toHaveLength(2)
  })

  it('renders nothing when transcript is empty', () => {
    const { container } = render(<TranscriptReview transcript="" noteLinks={[]} />)
    expect(container.firstChild).toBeNull()
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/__tests__/TranscriptReview.test.tsx`
Expected: FAIL (module not found)

**Step 3: Implement the component**

```tsx
// frontend/src/components/TranscriptReview.tsx
import type { NoteLink } from '../api-types.gen'

interface TranscriptReviewProps {
  transcript: string
  noteLinks: NoteLink[]
}

export default function TranscriptReview({ transcript, noteLinks }: TranscriptReviewProps) {
  if (!transcript) return null

  return (
    <div className="transcript-review">
      <div className="transcript-review-layout">
        <div className="transcript-review-text">
          <h4 className="transcript-review-heading">Transcript</h4>
          <p className="transcript-review-body">{transcript}</p>
        </div>
        {noteLinks.length > 0 && (
          <div className="transcript-review-students">
            <h4 className="transcript-review-heading">Extracted Notes</h4>
            <ul className="transcript-review-list">
              {noteLinks.map((link) => (
                <li key={link.noteId} className="transcript-review-student">
                  <span className="transcript-review-student-name">{link.name}</span>
                  <span className="transcript-review-student-class">{link.className}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </div>
  )
}
```

**Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/__tests__/TranscriptReview.test.tsx`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/components/TranscriptReview.tsx frontend/src/components/__tests__/TranscriptReview.test.tsx
git commit -m "feat: add TranscriptReview component"
```

---

### Task 4: Integrate transcript toggle into DoneJobCard

**Files:**
- Modify: `frontend/src/components/JobStatus.tsx`
- Modify: `frontend/src/components/__tests__/JobStatus.test.tsx`

**Context:** `DoneJobCard` takes `job: UploadJob` (which is a type alias for `VoiceNoteJob`). The component is defined inside `JobStatus.tsx`. It already renders `job.noteLinks` as clickable buttons and has a dismiss button.

**Step 1: Write a test for the toggle**

Add to existing `frontend/src/components/__tests__/JobStatus.test.tsx`. Follow existing patterns — the file uses `vi.mock('../../api', ...)`, `mockFetchJobs`, and `JobListResponse` type. The test should verify:
- A "View transcript" button appears on done jobs that have a transcript
- Clicking it reveals transcript text
- No button appears when transcript is missing/empty

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/__tests__/JobStatus.test.tsx`
Expected: FAIL

**Step 3: Implement the toggle in DoneJobCard**

In the `DoneJobCard` component inside `JobStatus.tsx`:

1. Add import for `TranscriptReview`
2. Add state: `const [showTranscript, setShowTranscript] = useState(false)`
3. After the `job-note-links` div, add a "View transcript" button (only when `job.transcript` is truthy)
4. Below, conditionally render `<TranscriptReview>` when `showTranscript` is true
5. Wrap in `AnimatePresence` + `motion.div` for smooth expand/collapse (consistent with existing animation patterns in the file)

```tsx
{job.transcript && (
  <>
    <button
      className="btn-text job-transcript-toggle"
      onClick={() => setShowTranscript(v => !v)}
    >
      {showTranscript ? 'Hide transcript' : 'View transcript'}
    </button>
    <AnimatePresence>
      {showTranscript && (
        <motion.div
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: 'auto' }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.2 }}
        >
          <TranscriptReview
            transcript={job.transcript}
            noteLinks={job.noteLinks ?? []}
          />
        </motion.div>
      )}
    </AnimatePresence>
  </>
)}
```

**Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/__tests__/JobStatus.test.tsx`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/components/JobStatus.tsx frontend/src/components/__tests__/JobStatus.test.tsx
git commit -m "feat: add transcript toggle to completed voice note jobs"
```

---

### Task 5: Add CSS for transcript review

**Files:**
- Modify: the CSS file where `.job-card` styles live (find with `rg "job-card" frontend/src/ -l -g "*.css"`)

**Step 1: Find the CSS file**

Run: `rg "job-card" frontend/src/ -l -g "*.css"`

**Step 2: Add styles**

Follow the design system (see `frontend/DESIGN.md`): `--chalk` backgrounds, `--comb` borders, `--ink`/`--ink-muted` text, `--font-body`, `12px` border-radius.

```css
/* Transcript review panel */
.transcript-review {
  margin-top: 8px;
  padding: 12px;
  background: var(--parchment);
  border-radius: 8px;
  border: 1px solid var(--comb);
}

.transcript-review-layout {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

@media (max-width: 600px) {
  .transcript-review-layout {
    grid-template-columns: 1fr;
  }
}

.transcript-review-heading {
  font-family: var(--font-display);
  font-weight: 500;
  font-size: 0.85rem;
  color: var(--ink);
  margin: 0 0 8px 0;
}

.transcript-review-body {
  font-size: 0.85rem;
  color: var(--ink-muted);
  line-height: 1.5;
  white-space: pre-wrap;
  margin: 0;
}

.transcript-review-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.transcript-review-student {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 8px;
  background: var(--chalk);
  border-radius: 6px;
  border: 1px solid var(--comb);
}

.transcript-review-student-name {
  font-size: 0.85rem;
  font-weight: 500;
  color: var(--ink);
}

.transcript-review-student-class {
  font-size: 0.75rem;
  color: var(--ink-muted);
}

.job-transcript-toggle {
  font-size: 0.8rem;
  color: var(--honey-dark);
  margin-top: 6px;
}
```

**Step 3: Verify visually**

Run the app locally and upload a voice note. After processing, the done card should show "View transcript" that expands to a two-column layout.

**Step 4: Commit**

```bash
git add frontend/src/<css-file>
git commit -m "style: transcript review panel styles"
```

---

### Task 6: Final verification

**Step 1: Run all backend tests**

Run: `cd backend && make test`
Expected: PASS

**Step 2: Run all frontend tests**

Run: `cd frontend && npx vitest run`
Expected: PASS

**Step 3: Run backend lint**

Run: `cd backend && make lint`
Expected: PASS

**Step 4: Commit any remaining changes and squash/rebase if desired**

---

## Open Questions

1. **Transcript retention** — The transcript currently lives only in-memory on the job (lost on dismiss/restart). The `voice_notes` DB table does **not** have a `transcript` column (the `transcript TEXT` column in `001_init.sql` is on the `notes` table, not `voice_notes`). Each per-student note already stores the full transcript for "Show transcript" in the notes list, so the source text is persisted there. Adding a `transcript` column to `voice_notes` would let the job review view survive restarts, but since it's an ephemeral "review and dismiss" flow, this may not be needed. Defer unless we find a use case.
2. **Note summaries** — The current plan shows student names + class in the right column. Should we also show the extracted summary text per student? That would require adding the summary to `NoteLink` (currently only has name/noteId/studentId/className).
3. ~~**Side-by-side vs stacked**~~ — Resolved: two-column grid, stacks on mobile.
4. **Actions on review** — Currently the review panel is read-only. The only actions on a done card are dismiss (whole job) and click student name (opens notes modal). There's no way to delete an individual extracted note from the review — you'd have to navigate to that student's notes list. Should we add per-note delete buttons in the review panel? Or is the current flow (review → if bad, go to student → delete) sufficient for v1?
