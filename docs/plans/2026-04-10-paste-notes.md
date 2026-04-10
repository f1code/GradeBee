# Paste Notes Feature

## Goal

Allow teachers to paste a block of text containing notes about multiple students/classes/dates. The system extracts and creates individual per-student notes using the existing AI extraction pipeline (skipping transcription).

Also redesign the "Add Notes" input section so the audio drop zone and secondary actions (Drive import, paste text) feel unified and nothing gets lost.

## UI Design

Rename "Upload Audio" → "Add Notes". Below the drop zone, replace the lonely "Add from Drive" button with a **row of two equal action buttons**:

```
┌──────────────────────────────────────┐
│            Add Notes                 │
│  ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─┐   │
│  │  🎙️ Drop audio or click to    │   │
│  │     browse                    │   │
│  └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─┘   │
│                                      │
│  [📁 Add from Drive] [📋 Paste text] │  ← equal-width secondary buttons
│                                      │
│  ┌──────────────────────────────┐    │  ← slides open when "Paste text"
│  │ Paste your notes here...     │    │    is clicked
│  │                              │    │
│  └──────────────────────────────┘    │
│  Include student names and dates —   │
│  we'll sort them out.                │
│            [Process Notes]           │
└──────────────────────────────────────┘
```

**Mobile (≤640px):** The drop zone is already replaced by stacked buttons. Add "Paste text" as a third stacked button. The textarea slides open below when tapped.

## Proposed Changes

### Backend

**`backend/handler.go`** — Add route `POST /text-notes/upload`

**`backend/text_notes.go`** (new) — `handleTextNotesUpload`:
- Accepts JSON `{ "text": "..." }`
- Creates a `VoiceNoteJob` with status "extracting" (skips transcription)
- Stores the text as the "transcript" on the voice_notes row ( reusing voice_notes with a null audio path may be simpler)
- Enqueues the job into the existing `MemQueue` pipeline, entering at the extraction step

**`backend/voice_note_process.go`** — Modify `processVoiceNote` to skip transcription if the job already has a transcript (i.e. text input). Alternatively, create a slimmer `processTextNote` that calls extract + create notes directly.

### Frontend

**`frontend/src/api.ts`** — Add `submitTextNotes(text: string, getToken)` calling `POST /text-notes/upload`.

**`frontend/src/components/AudioUpload.tsx`** — Rename to `AddNotes.tsx` (or keep file, rename component):
- Change heading from "Upload Audio" to "Add Notes"
- Replace `.drive-import-row` with a `.secondary-actions` row containing two equal buttons: "Add from Drive" and "Paste text"
- "Paste text" toggles a `<textarea>` + "Process Notes" button with slide animation
- On submit, call `submitTextNotes`, show same uploading/success states
- Mobile: add "Paste text" as third stacked button

**`frontend/src/index.css`** — Update styles:
- `.drive-import-row` → `.secondary-actions` (flex row, gap, equal-width buttons)
- Add `.paste-area` styles: textarea matching `--comb` bg, `--honey` focus ring, `var(--radius)` border-radius
- Mobile: `.secondary-actions` stacks vertically

### Backend architecture doc

**`backend/ARCHITECTURE.md`** — Add `POST /text-notes/upload` to the route table.

## Resolved Questions

1. **Reuse `voice_notes` table or new table?** → Reuse with nullable `audio_path`. Simpler, same job status UI works.
2. **Max text length?** → Cap at 50KB. Generous for pasted text, prevents abuse.
3. **Job status label?** → Skip straight to "extracting". The `JobStatus` UI already just renders current status, so this should work without changes.
