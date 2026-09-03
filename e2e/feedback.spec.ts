import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, type Page } from '@playwright/test'

// Implicit feedback signals on auto-extracted notes: the backend reads an edit
// or a delete of an auto note as a judgement on the extraction, so what the
// browser owes is the request itself. Explicit thumbs on a generated report are
// covered by reports.spec.ts; thumbs on a note card by NotesListFeedback.test.tsx.

// ---------------------------------------------------------------------------
// Shared route helpers
// ---------------------------------------------------------------------------

async function mockBaseRoutes(page: Page) {
  // Classes
  await page.route('**/classes', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          classes: [{ id: 1, levelId: 1, name: 'Grade 3A · Mon', levelName: 'Grade 3A', day: 'Monday', timeSlot: '', studentCount: 1 }],
        }),
      })
    } else {
      await route.continue()
    }
  })
  // Students
  await page.route('**/classes/1/students', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        students: [{ id: 10, classId: 1, name: 'Alice', createdAt: '2026-01-01T00:00:00Z' }],
      }),
    })
  })
  // Aliases — StudentDetail fetches notes and aliases together, so an unstubbed
  // aliases call fails the pair and the panel never renders its notes.
  await page.route('**/students/*/aliases', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ aliases: [] }) })
    } else {
      await route.continue()
    }
  })
  // Jobs (empty — avoid noise)
  await page.route('**/jobs', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ active: [], failed: [], done: [] }) })
  })
  // Levels (feeds the AddClassForm dropdown)
  await page.route('**/levels', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ levels: [{ id: 1, name: 'Grade 3A', groupId: 'g1', createdAt: '' }] }) })
  })
}

function autoNote(id: number, summary: string) {
  return { id, studentId: 10, date: '2026-01-15', summary, transcript: 'raw transcript', source: 'auto', createdAt: '2026-01-15T10:00:00Z', updatedAt: '2026-01-15T10:00:00Z' }
}

function manualNote(id: number, summary: string) {
  return { id, studentId: 10, date: '2026-01-15', summary, transcript: null, source: 'manual', createdAt: '2026-01-15T10:00:00Z', updatedAt: '2026-01-15T10:00:00Z' }
}

/** Expand class 1 and open Alice's detail panel on the notes tab. */
async function goToAliceNotes(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('student-list')).toBeVisible({ timeout: 10_000 })

  // Students sit behind a collapsed class row.
  await page.getByTestId('class-toggle-1').click()
  await expect(page.getByTestId('student-10')).toBeVisible()

  await page.getByTestId('student-10').getByText('Alice').click()
  await expect(page.getByTestId('student-detail-10')).toBeVisible({ timeout: 10_000 })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
  await mockBaseRoutes(page)
})

test.describe('Feedback — implicit signals on auto notes', () => {
  test('editing an auto note issues the PUT the backend reads as an edited signal', async ({ page }) => {
    // The refetch after saving reads this, so the edit has to land here for the
    // updated text to reach the list.
    let current = autoNote(55, 'Original auto note text')

    await page.route('**/students/10/notes', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [current] }) })
    })

    const editRequests: string[] = []
    await page.route('**/notes/55', async (route) => {
      if (route.request().method() === 'PUT') {
        editRequests.push(route.request().postData() ?? '')
        current = { ...current, summary: 'Updated auto note text', updatedAt: '2026-01-16T10:00:00Z' }
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(current) })
      } else {
        await route.continue()
      }
    })

    // Capture feedback call
    const feedbackRequests: string[] = []
    await page.route('**/feedback', async (route) => {
      if (route.request().method() === 'POST') {
        feedbackRequests.push(route.request().postData() ?? '')
        await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ id: 2, created_at: '2026-01-16T00:00:00Z' }) })
      } else {
        await route.continue()
      }
    })

    await goToAliceNotes(page)

    // Edit the note
    await page.getByTestId('edit-note-55').click()
    await page.getByTestId('note-editor-textarea').fill('Updated auto note text')
    await page.getByTestId('note-editor-save').click()

    await expect(page.getByTestId('note-55')).toContainText('Updated auto note text')

    // The `edited` signal is raised server-side by the PUT handler, so what the
    // browser owes is the PUT itself — with the new text on it. It posts no
    // feedback of its own for an edit.
    expect(editRequests).toHaveLength(1)
    expect(JSON.parse(editRequests[0]).summary).toBe('Updated auto note text')
    expect(feedbackRequests).toHaveLength(0)
  })

  test('deleting an auto note triggers delete with confirm dialog', async ({ page }) => {
    const note = autoNote(56, 'Auto note to be deleted')

    // Flipped by the DELETE handler, so the refetch that follows comes back
    // empty. One handler per URL — a second registration would shadow this one.
    let deleted = false
    await page.route('**/students/10/notes', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ notes: deleted ? [] : [note] }),
      })
    })

    const deleteRequests: string[] = []
    await page.route('**/notes/56', async (route) => {
      if (route.request().method() === 'DELETE') {
        deleteRequests.push(route.request().url())
        deleted = true
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'deleted' }) })
      } else {
        await route.continue()
      }
    })

    await goToAliceNotes(page)
    await expect(page.getByTestId('note-56')).toBeVisible({ timeout: 5_000 })

    // An auto note does carry thumbs — the anchor for the manual-note test below.
    await expect(page.getByTestId('thumb-up-note-56')).toHaveCount(1)

    // Confirmation is a second step — the trash icon alone deletes nothing.
    await page.getByTestId('delete-note-56').click()
    await expect(page.getByTestId('confirm-delete-note-56')).toBeVisible()
    expect(deleteRequests).toHaveLength(0)

    await page.getByTestId('confirm-delete-note-56').click()

    // The card is gone and the DELETE was issued exactly once.
    await expect(page.getByTestId('notes-empty')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('note-56')).toHaveCount(0)
    expect(deleteRequests).toHaveLength(1)
  })

  test('a manual note offers no thumbs buttons', async ({ page }) => {
    const note = manualNote(57, 'Manual note content')

    await page.route('**/students/10/notes', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [note] }) })
    })

    await goToAliceNotes(page)
    await expect(page.getByTestId('note-57')).toBeVisible({ timeout: 5_000 })

    // Manual notes are the teacher's own words — nothing to rate.
    await expect(page.getByTestId('thumb-up-note-57')).toHaveCount(0)
    await expect(page.getByTestId('thumb-down-note-57')).toHaveCount(0)
  })
})
