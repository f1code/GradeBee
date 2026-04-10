# Keyboard Focus Improvements Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Improve keyboard UX when adding classes, students, and using the Paste Text box.

**Architecture:** Small focus/scroll changes in three components — StudentList (auto-expand new class), AddStudentForm (re-focus after add), AudioUpload (auto-focus and scroll paste textarea).

**Tech Stack:** React refs, useEffect, scrollIntoView

---

### Task 1: Auto-expand new class and focus student name input

**Files:**
- Modify: `frontend/src/components/StudentList.tsx`
- Test: `frontend/src/components/__tests__/StudentList.test.tsx`

**Step 1: Write the failing test**

Add a test that verifies: after creating a class, the class is expanded and the add-student-input is visible.

```tsx
it('expands newly created class and shows add-student form', async () => {
  // Setup: render with one existing class, click "+ Add Class", submit
  // Assert: the new class group is expanded (student list visible)
  // Assert: add-student-input is in the document
})
```

The key behavioral assertion: after `handleClassCreated` runs, `expandedClassIds` should include the new class ID, and `expandedStudents` should be initialized to `[]` for it.

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/__tests__/StudentList.test.tsx --reporter=verbose`
Expected: FAIL — new class is not expanded after creation

**Step 3: Implement**

In `StudentList.tsx`, modify `handleClassCreated`:

```tsx
function handleClassCreated(cls: ClassItem) {
  setClasses(prev => [...prev, cls].sort((a, b) => a.name.localeCompare(b.name)))
  setShowAddClass(false)
  // Auto-expand the new class and initialize empty student list
  setExpandedClassIds(prev => new Set(prev).add(cls.id))
  setExpandedStudents(prev => new Map(prev).set(cls.id, []))
}
```

Also update the empty-state `onCreated` handler similarly:

```tsx
<AddClassForm onCreated={cls => {
  setClasses([cls])
  setExpandedClassIds(new Set([cls.id]))
  setExpandedStudents(new Map([[cls.id, []]]))
}} />
```

The `AddStudentForm` already auto-focuses its input via `useEffect(() => { inputRef.current?.focus() }, [])`, so expanding the class will mount the form which will auto-focus.

**Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/__tests__/StudentList.test.tsx --reporter=verbose`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/components/StudentList.tsx frontend/src/components/__tests__/StudentList.test.tsx
git commit -m "feat: auto-expand new class and focus student input"
```

---

### Task 2: Re-focus student name input after adding a student

**Files:**
- Modify: `frontend/src/components/AddStudentForm.tsx`
- Test: `frontend/src/components/__tests__/AddStudentForm.test.tsx`

**Step 1: Verify existing behavior**

The code already does `inputRef.current?.focus()` after successful creation in `handleSubmit`. Check the existing test file for coverage. If there's no test for re-focus, add one:

```tsx
it('clears input and re-focuses after successful submission', async () => {
  // Submit a name, wait for API, assert input is focused and empty
})
```

**Step 2: Run test**

Run: `cd frontend && npx vitest run src/components/__tests__/AddStudentForm.test.tsx --reporter=verbose`

This task may already be working. If the test passes with no changes, just add the test and commit. If focus isn't working (e.g., due to React state batching), wrap in `requestAnimationFrame`:

```tsx
// In handleSubmit, after setName(''):
requestAnimationFrame(() => inputRef.current?.focus())
```

**Step 3: Commit**

```bash
git add frontend/src/components/AddStudentForm.tsx frontend/src/components/__tests__/AddStudentForm.test.tsx
git commit -m "feat: ensure student input re-focuses after add"
```

---

### Task 3: Auto-focus and scroll paste textarea into view

**Files:**
- Modify: `frontend/src/components/AudioUpload.tsx`
- Test: `frontend/src/components/__tests__/AudioUpload.test.tsx`

**Step 1: Write the failing test**

```tsx
it('focuses paste textarea and scrolls into view when Paste Text is clicked', async () => {
  // Click "Paste Text" button
  // Wait for paste-textarea to appear
  // Assert: document.activeElement === paste-textarea element
})
```

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/__tests__/AudioUpload.test.tsx --reporter=verbose`
Expected: FAIL — textarea is not focused

**Step 3: Implement**

Add a ref and effect to the paste textarea in `AudioUpload.tsx`:

```tsx
const pasteRef = useRef<HTMLTextAreaElement>(null)

// Add useEffect watching showPaste:
useEffect(() => {
  if (showPaste && pasteRef.current) {
    pasteRef.current.focus()
    pasteRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }
}, [showPaste])
```

Add the ref to the textarea:

```tsx
<textarea
  ref={pasteRef}
  className="paste-textarea"
  ...
```

Note: Because the textarea is inside an AnimatePresence animation, the element may not be in the DOM immediately when `showPaste` becomes true. If the `useEffect` fires before the animation mounts, use `requestAnimationFrame`:

```tsx
useEffect(() => {
  if (showPaste) {
    requestAnimationFrame(() => {
      pasteRef.current?.focus()
      pasteRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    })
  }
}, [showPaste])
```

**Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/__tests__/AudioUpload.test.tsx --reporter=verbose`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/components/AudioUpload.tsx frontend/src/components/__tests__/AudioUpload.test.tsx
git commit -m "feat: auto-focus and scroll paste textarea into view"
```

---

### Task 4: Lint & final verification

**Step 1:** Run all frontend tests:
```bash
cd frontend && npx vitest run --reporter=verbose
```

**Step 2:** Commit if any fixups needed.
