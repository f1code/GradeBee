# Note Link → Student Detail Modal

## Goal

When an upload job completes and shows "1 note created — Maxence", clicking the student name opens a modal showing that student's notes (via `StudentDetail`). Currently the link is dead because the frontend expects a URL but the backend sends a noteId.

## Proposed Changes

### Backend

**`backend/upload_queue.go`** — Add `StudentID` and `ClassName` to `NoteLink`:

```go
type NoteLink struct {
    Name      string `json:"name"`
    NoteID    int64  `json:"noteId"`
    StudentID int64  `json:"studentId"`
    ClassName string `json:"className"`
}
```

**`backend/upload_process.go`** (~line 147) — Populate new fields (`studentID` is already resolved; `student.Class` has the class name):

```go
noteLinks = append(noteLinks, NoteLink{
    Name: student.Name, NoteID: result.NoteID,
    StudentID: studentID, ClassName: student.Class,
})
```

**Tests** — Update `NoteLink` literals in `upload_process_test.go`, `mem_queue_test.go`, `integration_test.go`.

### Frontend

**`frontend/src/api.ts`** — Fix `noteLinks` type:

```ts
noteLinks?: { name: string; noteId: number; studentId: number; className: string }[]
```

**`frontend/src/components/JobStatus.tsx`**:
- Add state: `const [modalStudent, setModalStudent] = useState<{id: number; name: string; className: string} | null>(null)`
- Replace `<a href={link.url}>` with `<button onClick={() => setModalStudent({id: link.studentId, name: link.name, className: link.className})}>` 
- Render modal overlay (follow `HowItWorks` pattern) containing `<StudentDetail>` when `modalStudent` is set. Pass `onCollapse={() => setModalStudent(null)}`.

**`frontend/src/index.css`** — Add modal styles for the student detail modal (overlay + card), reusing the `how-it-works-overlay` pattern.

**Tests** — Update `JobStatus.test.tsx`: fix noteLink shape, add test that clicking a note link renders StudentDetail in modal.

## Summary

Self-contained in `JobStatus` — no cross-component wiring needed. The modal reuses `StudentDetail` as-is. Backend change is minimal (two extra fields on `NoteLink`, values already available).

**`frontend/src/components/StudentDetail.tsx`** — Add optional `modal?: boolean` prop. When true, hide the "← Back to list" button.
