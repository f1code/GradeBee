# ItemRow Shared Component — Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Extract the repeated row pattern (name + chevron, hover-reveal actions, delete confirmation, expand/collapse animation) into a shared `ItemRow` component. Adopt it in ReportExamples first, then refactor StudentList to use it.

**Architecture:** Create `ItemRow` in its own file. It owns: row layout, chevron toggle, action button slots, delete confirmation flow, and expand/collapse `AnimatePresence`. Consumers pass `name`, `expanded`, `onToggle`, `onDelete`, optional `actions` slot, and `children` for expanded content. CSS uses new generic `.item-row-*` classes. After adoption in both components, remove the now-dead CSS (`.example-delete-btn`, `.student-row`, `.student-actions`, `.student-name-clickable`, etc.).

**Tech Stack:** React (TypeScript), Motion (motion/react), CSS, Vitest

---

### Task 1: Create the ItemRow component with tests

**Files:**
- Create: `frontend/src/components/ItemRow.tsx`
- Create: `frontend/src/components/__tests__/ItemRow.test.tsx`

**Step 1: Write the test**

**File:** `frontend/src/components/__tests__/ItemRow.test.tsx`

```tsx
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import ItemRow from '../ItemRow'

describe('ItemRow', () => {
  it('renders name and chevron', () => {
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={vi.fn()}>
        <p>Details</p>
      </ItemRow>
    )
    expect(screen.getByText('Test Item')).toBeInTheDocument()
    // Chevron SVG is present
    expect(screen.getByLabelText('Delete Test Item')).toBeInTheDocument()
  })

  it('calls onToggle when name is clicked', async () => {
    const onToggle = vi.fn()
    const user = userEvent.setup()
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={onToggle} onDelete={vi.fn()}>
        <p>Details</p>
      </ItemRow>
    )
    await user.click(screen.getByText('Test Item'))
    expect(onToggle).toHaveBeenCalledOnce()
  })

  it('shows children when expanded', () => {
    render(
      <ItemRow name="Test Item" expanded={true} onToggle={vi.fn()} onDelete={vi.fn()}>
        <p>Expanded content here</p>
      </ItemRow>
    )
    expect(screen.getByText('Expanded content here')).toBeInTheDocument()
  })

  it('hides children when collapsed', () => {
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={vi.fn()}>
        <p>Expanded content here</p>
      </ItemRow>
    )
    expect(screen.queryByText('Expanded content here')).not.toBeInTheDocument()
  })

  it('shows delete confirmation when trash is clicked, then calls onDelete on confirm', async () => {
    const onDelete = vi.fn()
    const user = userEvent.setup()
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={onDelete}>
        <p>Details</p>
      </ItemRow>
    )

    // Click trash icon
    await user.click(screen.getByLabelText('Delete Test Item'))

    // Confirmation appears
    await waitFor(() => {
      expect(screen.getByText(/Delete/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /Cancel/ })).toBeInTheDocument()
    })

    // Confirm delete
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onDelete).toHaveBeenCalledOnce()
  })

  it('cancels delete confirmation', async () => {
    const onDelete = vi.fn()
    const user = userEvent.setup()
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={onDelete}>
        <p>Details</p>
      </ItemRow>
    )

    await user.click(screen.getByLabelText('Delete Test Item'))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Cancel/ })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: /Cancel/ }))
    // Confirmation gone, onDelete not called
    expect(onDelete).not.toHaveBeenCalled()
    // Row is back
    expect(screen.getByText('Test Item')).toBeInTheDocument()
  })

  it('renders extra actions via actions prop', () => {
    render(
      <ItemRow
        name="Test Item"
        expanded={false}
        onToggle={vi.fn()}
        onDelete={vi.fn()}
        actions={<button aria-label="Edit">✏️</button>}
      >
        <p>Details</p>
      </ItemRow>
    )
    expect(screen.getByLabelText('Edit')).toBeInTheDocument()
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd frontend && npx vitest run src/components/__tests__/ItemRow.test.tsx
```

Expected: FAIL — module not found.

**Step 3: Implement the component**

**File:** `frontend/src/components/ItemRow.tsx`

```tsx
import { useState, type ReactNode } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { ChevronIcon, TrashIcon } from './Icons'

interface ItemRowProps {
  name: string
  expanded: boolean
  onToggle: () => void
  onDelete: () => void
  actions?: ReactNode
  children: ReactNode
}

export default function ItemRow({
  name,
  expanded,
  onToggle,
  onDelete,
  actions,
  children,
}: ItemRowProps) {
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  function handleDeleteClick(e: React.MouseEvent) {
    e.stopPropagation()
    setConfirmingDelete(true)
  }

  function handleConfirm() {
    setConfirmingDelete(false)
    onDelete()
  }

  function handleCancel() {
    setConfirmingDelete(false)
  }

  if (confirmingDelete) {
    return (
      <div className="delete-confirm delete-confirm-inline">
        <span>
          Delete <strong>{name}</strong>?
        </span>
        <div className="delete-confirm-actions">
          <button className="btn-secondary btn-sm" onClick={handleCancel}>
            Cancel
          </button>
          <button className="btn-danger btn-sm" onClick={handleConfirm}>
            Delete
          </button>
        </div>
      </div>
    )
  }

  return (
    <>
      <div className="item-row">
        <span
          className={`item-row-name${expanded ? ' item-row-name-active' : ''}`}
          onClick={onToggle}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              onToggle()
            }
          }}
        >
          {name}
          <ChevronIcon open={expanded} />
        </span>
        <div className="item-row-actions">
          {actions}
          <button
            className="icon-btn icon-btn-danger"
            onClick={handleDeleteClick}
            aria-label={`Delete ${name}`}
          >
            <TrashIcon />
          </button>
        </div>
      </div>
      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.15 }}
            style={{ overflow: 'hidden' }}
          >
            {children}
          </motion.div>
        )}
      </AnimatePresence>
    </>
  )
}
```

**Step 4: Run test to verify it passes**

```bash
cd frontend && npx vitest run src/components/__tests__/ItemRow.test.tsx
```

Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/components/ItemRow.tsx frontend/src/components/__tests__/ItemRow.test.tsx
git commit -m "feat: create ItemRow shared component with delete confirmation"
```

---

### Task 2: Add CSS for ItemRow

**Files:**
- Modify: `frontend/src/index.css`

**Step 1: Add item-row styles**

In `frontend/src/index.css`, find the comment `/* --- Icon buttons (pencil / trash) --- */` (around line 414). Insert the following **before** that comment:

```css
/* --- Expandable row (shared pattern) --- */

.item-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.item-row-name {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.9rem;
  color: var(--ink);
  text-decoration-color: transparent;
  transition: color 0.15s, text-decoration-color 0.15s;
}

.item-row-name:hover {
  color: var(--honey-dark);
  text-decoration: underline;
  text-decoration-color: rgba(232, 163, 23, 0.4);
  text-underline-offset: 2px;
}

.item-row-name:focus-visible {
  outline: 2px solid var(--honey);
  outline-offset: 2px;
  border-radius: 4px;
}

.item-row-name-active {
  color: var(--honey-dark);
}

.item-row-actions {
  display: flex;
  align-items: center;
  gap: 0.15rem;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.item-row:hover .item-row-actions {
  opacity: 1;
}

```

**Step 2: Run tests**

```bash
cd frontend && npx vitest run src/components/__tests__/ItemRow.test.tsx
```

Expected: PASS

**Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "style: add item-row CSS classes"
```

---

### Task 3: Adopt ItemRow in ReportExamples

**Files:**
- Modify: `frontend/src/components/ReportExamples.tsx`
- Modify: `frontend/src/components/__tests__/ReportExamples.test.tsx`

**Step 1: Update the test**

**File:** `frontend/src/components/__tests__/ReportExamples.test.tsx`

Replace entire file:

```tsx
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', () => ({
  listReportExamples: vi.fn().mockResolvedValue({
    examples: [
      { id: '1', name: 'Report.jpg', content: 'Student showed great improvement in math.' },
    ],
  }),
  uploadReportExample: vi.fn(),
  deleteReportExample: vi.fn(),
  importExampleFromDrive: vi.fn(),
  getGoogleToken: vi.fn(),
}))

vi.mock('../../hooks/useDrivePicker', () => ({
  useDrivePicker: () => ({ openPicker: vi.fn() }),
}))

describe('ReportExamples', () => {
  it('renders toggle button', async () => {
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)
    expect(screen.getByText(/Example Report Cards/)).toBeInTheDocument()
  })

  it('shows extracted text when example is clicked', async () => {
    const user = userEvent.setup()
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)

    // Expand the examples section
    await user.click(screen.getByText(/Example Report Cards/))

    // Wait for the example to appear
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
    })

    // Content should not be visible yet
    expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()

    // Click the example name to expand it
    await user.click(screen.getByText('Report.jpg'))

    // Content should now be visible
    await waitFor(() => {
      expect(screen.getByText(/great improvement/)).toBeInTheDocument()
    })

    // Click again to collapse
    await user.click(screen.getByText('Report.jpg'))
    await waitFor(() => {
      expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()
    })
  })

  it('has a trash icon button for deleting', async () => {
    const user = userEvent.setup()
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)

    await user.click(screen.getByText(/Example Report Cards/))

    await waitFor(() => {
      expect(screen.getByLabelText('Delete Report.jpg')).toBeInTheDocument()
    })
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd frontend && npx vitest run src/components/__tests__/ReportExamples.test.tsx
```

Expected: FAIL — still renders the old `📄 Report.jpg` markup.

**Step 3: Update ReportExamples to use ItemRow**

**File:** `frontend/src/components/ReportExamples.tsx`

Add import at the top (alongside other imports):

```tsx
import ItemRow from './ItemRow'
```

Remove the import of `ChevronIcon` and `TrashIcon` if present (the shared component imports them internally).

Replace the `examples.map` block (inside `<div className="example-list">`) with:

```tsx
{examples.map((ex) => (
  <motion.div
    key={ex.id}
    className="example-item-wrapper"
    initial={{ opacity: 0, x: -10 }}
    animate={{ opacity: 1, x: 0 }}
  >
    <ItemRow
      name={ex.name}
      expanded={expandedId === ex.id}
      onToggle={() => setExpandedId(expandedId === ex.id ? null : ex.id)}
      onDelete={() => handleDelete(ex.id)}
    >
      <div className="example-content-preview">
        <pre className="example-content-text">{ex.content}</pre>
      </div>
    </ItemRow>
  </motion.div>
))}
```

Also remove the `AnimatePresence` import if it's no longer used directly in this file (it's still used for the collapse toggle, so keep it).

**Step 4: Run test to verify it passes**

```bash
cd frontend && npx vitest run src/components/__tests__/ReportExamples.test.tsx
```

Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/components/ReportExamples.tsx frontend/src/components/__tests__/ReportExamples.test.tsx
git commit -m "refactor: adopt ItemRow in ReportExamples"
```

---

### Task 4: Adopt ItemRow in StudentList

**Files:**
- Modify: `frontend/src/components/StudentList.tsx`

This is the trickiest task. The student row has inline editing (`editingStudentId`) and delete confirmation (`deletingId`) as conditional branches that replace the row. With `ItemRow`, delete confirmation is now internal to the component. Inline editing stays external (rendered instead of the row).

**Step 1: Run existing StudentList tests to establish baseline**

```bash
cd frontend && npx vitest run src/components/__tests__/StudentList.test.tsx
```

Expected: PASS — all existing tests pass before we change anything.

**Step 2: Update StudentList to use ItemRow**

**File:** `frontend/src/components/StudentList.tsx`

Add import:

```tsx
import ItemRow from './ItemRow'
```

Find the student `<motion.li>` block (the `students.map` callback, around lines 430-500). Replace the body of the `<motion.li>` (everything between the opening and closing tags) with:

```tsx
{editingStudentId === s.id ? (
  <InlineEdit
    value={s.name}
    onSave={newName => handleRenameStudent(s.id, cls.id, newName)}
    onCancel={() => setEditingStudentId(null)}
  />
) : (
  <ItemRow
    name={s.name}
    expanded={expandedStudentId === s.id}
    onToggle={() => setExpandedStudentId(expandedStudentId === s.id ? null : s.id)}
    onDelete={() => handleDeleteStudent(s.id, cls.id)}
    actions={
      <button
        className="icon-btn"
        onClick={e => { e.stopPropagation(); setEditingStudentId(s.id) }}
        aria-label={`Rename ${s.name}`}
        data-testid={`rename-student-${s.id}`}
      >
        <PencilIcon />
      </button>
    }
  >
    <StudentDetail
      studentId={s.id}
      studentName={s.name}
      className={cls.name}
      onCollapse={() => setExpandedStudentId(null)}
    />
  </ItemRow>
)}
```

This removes:
- The manual `isDeletingStudent` conditional (now inside `ItemRow`)
- The manual `student-row` / `student-actions` / `student-name-clickable` markup
- The manual `AnimatePresence` for expand/collapse

Also remove the `deletingId` state management for students — specifically:
- Remove the `isDeletingStudent` variable: `const isDeletingStudent = deletingId?.type === 'student' && deletingId.id === s.id`
- The `setDeletingId` call for students is no longer needed since `ItemRow` handles confirmation internally and calls `onDelete` directly.

**Important:** Keep the `deletingId` state and its class-level usage intact — only the student-level delete confirmation moves into `ItemRow`. The `handleDeleteStudent` function stays as-is (remove the `setDeletingId(null)` line at the top since it's no longer called from the confirmation dialog — it's called directly by `ItemRow` after confirmation).

**Step 3: Run StudentList tests**

```bash
cd frontend && npx vitest run src/components/__tests__/StudentList.test.tsx
```

If tests that check for `confirm-delete-student-*` data-testid fail, that's expected — the delete confirmation is now inside ItemRow and doesn't have that test ID. Update those specific tests to use the new flow (click trash via aria-label, then click "Delete" button in confirmation).

**Step 4: Run all tests**

```bash
cd frontend && npx vitest run
```

Expected: All pass.

**Step 5: Commit**

```bash
git add frontend/src/components/StudentList.tsx
git commit -m "refactor: adopt ItemRow in StudentList for student rows"
```

---

### Task 5: Clean up dead CSS

**Files:**
- Modify: `frontend/src/index.css`

**Step 1: Remove old styles that are now replaced by `.item-row-*`**

Remove these CSS rules (search for them):

- `.example-delete-btn` and `.example-delete-btn:hover` (replaced by `.icon-btn-danger`)
- `.example-name-clickable` and `.example-name-clickable:hover` (if they exist — they were from the earlier plan, may not have landed)
- `.example-actions` and `.example-item:hover .example-actions` (if they exist)
- `.student-row` (replaced by `.item-row`)
- `.student-actions` and `.class-group li:hover .student-actions` (replaced by `.item-row-actions`)
- `.student-name-clickable`, `.student-name-clickable:hover`, `.student-name-clickable:focus-visible`, `.student-name-active` (replaced by `.item-row-name-*`)

Keep `.student-name` (the overflow/ellipsis rule) — check if it's still referenced. If not, remove it too.

Keep `.student-row` responsive overrides in `@media` blocks if they reference `.student-actions` — update those to use `.item-row-actions` instead.

**Step 2: Verify no visual regressions by running all tests**

```bash
cd frontend && npx vitest run
```

Expected: All pass.

**Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "chore: remove dead CSS replaced by item-row classes"
```

---

### Task 6: Manual visual check — report examples

**STOP and ask the user to verify.** The dev server should be running.

Ask the user to check:
- Example report cards section: items show name + chevron, trash icon appears on hover, click expands content preview, delete shows confirmation
- Styling matches the student rows

Wait for feedback before proceeding.

---

### Task 7: Manual visual check — student list

**STOP and ask the user to verify.**

Ask the user to check:
- Student rows still look and behave the same: name + chevron, pencil + trash on hover, delete confirmation, expand to student detail
- No visual regressions

Wait for feedback before proceeding.

---

### Task 8: Final verification

**Step 1: Run backend lint**

```bash
cd backend && make lint
```

**Step 2: Run all frontend tests**

```bash
cd frontend && npx vitest run
```

**Step 3: Run e2e**

```bash
npx playwright test
```

Expected: All pass.
