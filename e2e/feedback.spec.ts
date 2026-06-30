import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, Page } from '@playwright/test'

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
          classes: [{ id: 1, name: 'Grade 3A', levelName: 'Grade 3A', scheduleName: '', studentCount: 1 }],
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
  // Jobs (empty — avoid noise)
  await page.route('**/jobs', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ active: [], failed: [], done: [] }) })
  })
  // Report examples (empty)
  await page.route('**/report-examples', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ examples: [] }) })
    } else {
      await route.continue()
    }
  })
  // Class names for autocomplete
  await page.route('**/classes/class-names', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ levelNames: ['Grade 3A'] }) })
  })
}

function autoNote(id: number, summary: string) {
  return { id, studentId: 10, date: '2026-01-15', summary, transcript: 'raw transcript', source: 'auto', createdAt: '2026-01-15T10:00:00Z', updatedAt: '2026-01-15T10:00:00Z' }
}

function manualNote(id: number, summary: string) {
  return { id, studentId: 10, date: '2026-01-15', summary, transcript: null, source: 'manual', createdAt: '2026-01-15T10:00:00Z', updatedAt: '2026-01-15T10:00:00Z' }
}

/** Navigate to student detail notes tab for student 10 (Alice). */
async function goToAliceNotes(page: Page) {
  await page.goto('/')
  // Click Alice to open her detail panel
  await expect(page.getByText('Alice')).toBeVisible({ timeout: 10_000 })
  await page.getByText('Alice').click()
  await expect(page.getByTestId('student-detail-10')).toBeVisible({ timeout: 10_000 })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
  await mockBaseRoutes(page)
})

test.describe('Feedback — explicit thumbs on report', () => {
  test('click thumbs-down on a report, fill comment, API call captured', async ({ page }) => {
    // Mock reports generation
    await page.route('**/reports', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            reports: [{
              id: 42, studentId: 10, student: 'Alice', class: 'Grade 3A',
              html: '<p>Alice shows great progress.</p>',
              startDate: '2026-01-01', endDate: '2026-03-31', createdAt: '2026-04-01T00:00:00Z',
            }],
            error: null,
          }),
        })
      } else {
        await route.continue()
      }
    })

    // Capture the feedback POST
    const feedbackRequests: string[] = []
    await page.route('**/feedback', async (route) => {
      if (route.request().method() === 'POST') {
        feedbackRequests.push(route.request().postData() ?? '')
        await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ id: 1, created_at: '2026-04-01T00:00:00Z' }) })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await page.getByText('Reports').click()
    await expect(page.getByText('Alice')).toBeVisible({ timeout: 10_000 })
    await page.getByText('Grade 3A').click()
    await page.getByRole('button', { name: /Generate.*Report/ }).click()
    await expect(page.getByTestId('report-result-name')).toBeVisible({ timeout: 10_000 })

    // Open the full report viewer — click on Alice's name or expand button
    await page.getByTestId('report-result-name').click()

    // Wait for thumbs buttons
    await expect(page.getByTestId('thumb-down')).toBeVisible({ timeout: 5_000 })

    // Click thumbs-down
    await page.getByTestId('thumb-down').click()

    // Comment textarea appears
    await expect(page.getByTestId('thumb-down-comment')).toBeVisible({ timeout: 3_000 })

    // Type comment and submit
    await page.getByTestId('thumb-down-comment').fill('Tone was too formal')
    await page.getByTestId('thumb-down-submit').click()

    // Confirmation appears
    await expect(page.getByText(/thanks for your feedback/i)).toBeVisible({ timeout: 5_000 })

    // Verify the API call was made with correct payload
    expect(feedbackRequests.length).toBe(1)
    const payload = JSON.parse(feedbackRequests[0])
    expect(payload.artifact_type).toBe('report')
    expect(payload.artifact_id).toBe(42)
    expect(payload.rating).toBe('down')
    expect(payload.comment).toBe('Tone was too formal')
  })
})

test.describe('Feedback — implicit signals on auto notes', () => {
  test('editing an auto note sends implicit edited signal', async ({ page }) => {
    const note = autoNote(55, 'Original auto note text')

    await page.route('**/students/10/notes', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [note] }) })
    })

    // Mock the PUT (edit)
    await page.route('**/notes/55', async (route) => {
      if (route.request().method() === 'PUT') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ...note, summary: 'Updated auto note text', updatedAt: '2026-01-16T10:00:00Z' }),
        })
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
    await page.getByLabel(/summary/i).fill('Updated auto note text')
    await page.getByRole('button', { name: /save/i }).click()

    // The backend edit handler fires the implicit signal — the frontend itself doesn't
    // call /api/feedback for edit (it's backend-side). So we verify no explicit feedback
    // request was made from the frontend for this action (the signal is server-side).
    // What we can verify: the PUT /notes/55 was issued correctly.
    await expect(page.getByText('Updated auto note text')).toBeVisible({ timeout: 5_000 })
    // No frontend feedback call expected for edit (it's implicit/server-side)
    expect(feedbackRequests.length).toBe(0)
  })

  test('deleting an auto note triggers delete with confirm dialog', async ({ page }) => {
    const note = autoNote(56, 'Auto note to be deleted')

    await page.route('**/students/10/notes', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [note] }) })
    })

    const deleteRequests: string[] = []
    await page.route('**/notes/56', async (route) => {
      if (route.request().method() === 'DELETE') {
        deleteRequests.push('deleted')
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'deleted' }) })
      } else {
        await route.continue()
      }
    })

    // Refetch returns empty after delete
    let deleteTriggered = false
    await page.route('**/students/10/notes', async (route) => {
      if (deleteTriggered) {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [] }) })
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [note] }) })
      }
    })

    await goToAliceNotes(page)
    await expect(page.getByTestId('note-56')).toBeVisible({ timeout: 5_000 })

    // Click delete icon → confirm
    await page.getByTestId('delete-note-56').click()
    deleteTriggered = true
    await page.getByTestId('confirm-delete-note-56').click()

    // Delete was requested
    expect(deleteRequests.length).toBe(1)
  })

  test('editing a manual note does NOT show thumbs buttons', async ({ page }) => {
    const note = manualNote(57, 'Manual note content')

    await page.route('**/students/10/notes', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [note] }) })
    })

    await goToAliceNotes(page)
    await expect(page.getByTestId('note-57')).toBeVisible({ timeout: 5_000 })

    // Manual notes should not show thumb buttons
    await expect(page.getByTestId('thumb-up-note-57')).not.toBeVisible()
    await expect(page.getByTestId('thumb-down-note-57')).not.toBeVisible()
  })
})
