# Example Text Preview Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Let users view the extracted text content of uploaded example report cards, instead of only seeing the filename.

**Architecture:** Include `content` in the list API response (report card texts are small). Frontend gets an expandable preview on each example item — click to expand, click again to collapse. No new endpoints needed.

**Tech Stack:** Go backend (handler package), React frontend (TypeScript), Vitest for frontend tests, Go tests for backend.

---

### Task 1: Backend — return content in list response

**Files:**
- Modify: `backend/report_examples.go` (lines 41-44 in `ListExamples`)

**Step 1: Write the failing test**

Add a test in `backend/report_examples_handler_test.go` (create if needed) that verifies the list response includes `content`. If tests aren't straightforward to add for the handler, we can verify via the store layer instead. Check if there's an existing handler test pattern first — if not, test at the `dbExampleStore` level.

Actually, looking at the code, the simplest change is in `ListExamples` — it already has the content from the DB but strips it. We just need to include it.

**File:** `backend/report_examples.go`

Change `ListExamples` to include content:

```go
func (s *dbExampleStore) ListExamples(ctx context.Context, userID string) ([]ReportExample, error) {
	dbExamples, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("report_examples: list: %w", err)
	}
	examples := make([]ReportExample, len(dbExamples))
	for i, e := range dbExamples {
		examples[i] = ReportExample{ID: e.ID, Name: e.Name, Content: e.Content}
	}
	return examples, nil
}
```

**Step 2: Verify**

```bash
cd backend && make lint
```

Expected: PASS

**Step 3: Commit**

```bash
git add backend/report_examples.go
git commit -m "feat: include content in report examples list response"
```

---

### Task 2: Frontend API — add content to type

**Files:**
- Modify: `frontend/src/api.ts`

**Step 1: Update `ReportExampleItem` interface**

```typescript
export interface ReportExampleItem {
  id: string
  name: string
  content: string
}
```

**Step 2: Commit**

```bash
git add frontend/src/api.ts
git commit -m "feat: add content field to ReportExampleItem type"
```

---

### Task 3: Frontend — expandable text preview on example items

**Files:**
- Modify: `frontend/src/components/ReportExamples.tsx`
- Modify: `frontend/src/index.css`

**Step 1: Write the failing test**

**File:** `frontend/src/components/__tests__/ReportExamples.test.tsx`

Replace the mock and add a test:

```typescript
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

// Also mock the drive picker hook
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
      expect(screen.getByText('📄 Report.jpg')).toBeInTheDocument()
    })

    // Content should not be visible yet
    expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()

    // Click the example item to expand it
    await user.click(screen.getByText('📄 Report.jpg'))

    // Content should now be visible
    await waitFor(() => {
      expect(screen.getByText(/great improvement/)).toBeInTheDocument()
    })

    // Click again to collapse
    await user.click(screen.getByText('📄 Report.jpg'))
    await waitFor(() => {
      expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()
    })
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd frontend && npx vitest run src/components/__tests__/ReportExamples.test.tsx
```

Expected: FAIL — content preview not rendered

**Step 3: Implement expandable preview in ReportExamples.tsx**

In `ReportExamples.tsx`, add state for tracking which example is expanded, and render the content below the item when expanded.

Add state:
```typescript
const [expandedId, setExpandedId] = useState<string | null>(null)
```

Replace the example item rendering (the `examples.map` block) with:

```tsx
{examples.map((ex) => (
  <motion.div
    key={ex.id}
    className="example-item-wrapper"
    initial={{ opacity: 0, x: -10 }}
    animate={{ opacity: 1, x: 0 }}
    layout
  >
    <div className="example-item">
      <span
        className="example-name"
        onClick={() => setExpandedId(expandedId === ex.id ? null : ex.id)}
        style={{ cursor: 'pointer' }}
      >
        📄 {ex.name}
      </span>
      <button
        className="example-delete-btn"
        onClick={() => handleDelete(ex.id)}
        title="Remove example"
      >
        ✕
      </button>
    </div>
    <AnimatePresence>
      {expandedId === ex.id && (
        <motion.div
          className="example-content-preview"
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: 'auto', opacity: 1 }}
          exit={{ height: 0, opacity: 0 }}
          transition={{ duration: 0.15 }}
          style={{ overflow: 'hidden' }}
        >
          <pre className="example-content-text">{ex.content}</pre>
        </motion.div>
      )}
    </AnimatePresence>
  </motion.div>
))}
```

**Step 4: Add CSS styles**

In `frontend/src/index.css`, after the `.example-delete-btn:hover` rule, add:

```css
.example-item-wrapper {
  display: flex;
  flex-direction: column;
}

.example-content-preview {
  padding: 0.5rem 0.75rem;
  background: var(--chalk);
  border-top: 1px solid rgba(44, 24, 16, 0.08);
  border-radius: 0 0 8px 8px;
  margin-top: -0.2rem;
}

.example-content-text {
  font-size: 0.8rem;
  color: var(--ink-muted);
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
  max-height: 200px;
  overflow-y: auto;
  font-family: inherit;
  line-height: 1.5;
}
```

**Step 5: Run test to verify it passes**

```bash
cd frontend && npx vitest run src/components/__tests__/ReportExamples.test.tsx
```

Expected: PASS

**Step 6: Commit**

```bash
git add frontend/src/components/ReportExamples.tsx frontend/src/components/__tests__/ReportExamples.test.tsx frontend/src/index.css
git commit -m "feat: expandable text preview for example report cards"
```

---

### Task 4: Verify everything

**Step 1: Run backend lint**

```bash
cd backend && make lint
```

**Step 2: Run all frontend tests**

```bash
cd frontend && npx vitest run
```

**Step 3: Run e2e if available**

```bash
npx playwright test
```

Expected: All pass
