import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, type Page } from '@playwright/test'

/**
 * Note editor date coverage. A note's date is fixed once the note exists: the
 * edit path drops it at every layer below the editor (NotesList passes only the
 * summary, and the API client, handler and repo are summary-only), so the
 * editor must not offer a date control in edit mode. A `readonly` date input
 * would not do — Chrome still opens its calendar picker — so edit mode renders
 * static text and only create mode gets a real input.
 *
 * Backend is stubbed at the network edge, as in students.spec.ts /
 * move-student.spec.ts, so the run never touches the dev sqlite db.
 */

const CLASS_ID = 1
const ALICE = 101
const NOTE_ID = 55
const NOTE_DATE = '2026-01-15'
/** What formatNoteDate() renders NOTE_DATE as. */
const NOTE_DATE_LONG = 'January 15, 2026'

type Note = {
  id: number
  studentId: number
  date: string
  summary: string
  transcript: string | null
  source: 'auto' | 'manual'
  createdAt: string
  updatedAt: string
}

function manualNote(id: number, date: string, summary: string): Note {
  return {
    id,
    studentId: ALICE,
    date,
    summary,
    transcript: null,
    source: 'manual',
    createdAt: `${date}T10:00:00Z`,
    updatedAt: `${date}T10:00:00Z`,
  }
}

interface Stub {
  /** Raw bodies of every PUT /notes/{id}. */
  putBodies: string[]
  /** Raw bodies of every POST /students/{id}/notes. */
  postBodies: string[]
}

/**
 * Stubs the roster plus the student's notes. `notes` is mutated by the write
 * handlers so the refetch the UI does after a save shows the new state.
 */
async function mockApi(page: Page, notes: Note[]): Promise<Stub> {
  const stub: Stub = { putBodies: [], postBodies: [] }

  await page.route('**/classes', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          classes: [
            { id: CLASS_ID, levelId: 1, name: '5A · Mon', levelName: '5A', day: 'Monday', timeSlot: '', studentCount: 1 },
          ],
        }),
      })
    } else {
      await route.continue()
    }
  })

  await page.route('**/levels', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ levels: [{ id: 1, name: '5A', reportInstructions: '' }] }),
      })
    } else {
      await route.continue()
    }
  })

  await page.route(`**/classes/${CLASS_ID}/students`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ students: [{ id: ALICE, name: 'Alice' }] }),
    })
  })

  // GET lists the notes; POST creates one. Both arms must be served here —
  // route.continue() would reach the live backend, not an earlier handler.
  await page.route(`**/students/${ALICE}/notes`, async (route) => {
    const request = route.request()
    if (request.method() === 'POST') {
      stub.postBodies.push(request.postData() ?? '')
      const body = request.postDataJSON() as { date?: string; summary?: string }
      const created = manualNote(900, body.date ?? '', body.summary ?? '')
      notes.push(created)
      await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(created) })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ notes }),
    })
  })

  // The detail panel loads notes and aliases together; a failing alias fetch
  // puts the whole panel in its error state, so this one must be stubbed too.
  await page.route(`**/students/${ALICE}/aliases`, async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ aliases: [] }) })
  })

  await page.route(`**/notes/${NOTE_ID}`, async (route) => {
    const request = route.request()
    if (request.method() !== 'PUT') {
      await route.continue()
      return
    }
    stub.putBodies.push(request.postData() ?? '')
    const body = request.postDataJSON() as { summary?: string }
    const note = notes.find((n) => n.id === NOTE_ID)
    if (note && typeof body.summary === 'string') note.summary = body.summary
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(note),
    })
  })

  // Empty job lists so the poller stays out of the way.
  await page.route('**/jobs', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ active: [], failed: [], done: [] }),
      })
    } else {
      await route.continue()
    }
  })

  return stub
}

/** Expand class 1 and open Alice's detail panel on the notes tab. */
async function openAliceNotes(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('student-list')).toBeVisible({ timeout: 10000 })
  await page.getByTestId(`class-toggle-${CLASS_ID}`).click()
  await expect(page.getByTestId(`student-${ALICE}`)).toBeVisible()
  await page.getByTestId(`student-${ALICE}`).getByText('Alice').click()
  await expect(page.getByTestId(`student-detail-${ALICE}`)).toBeVisible({ timeout: 10000 })
}

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
})

test.describe('Note editing — the date is fixed', () => {
  test('edit mode shows the date as static text, with no editable date control', async ({ page }) => {
    await mockApi(page, [manualNote(NOTE_ID, NOTE_DATE, 'Original observation')])

    await openAliceNotes(page)
    await expect(page.getByTestId('notes-list')).toBeVisible({ timeout: 10000 })
    await page.getByTestId(`edit-note-${NOTE_ID}`).click()

    // Assert the editor is actually mounted first, so the absence checks below
    // cannot pass simply because nothing has rendered yet.
    await expect(page.getByTestId('note-editor-textarea')).toBeVisible()

    const editor = page.locator('.note-editor')
    const dateEl = page.getByTestId('note-editor-date')

    // The date reads back in the app's long format, not as a form value.
    await expect(dateEl).toHaveText(NOTE_DATE_LONG)
    await expect(dateEl).toHaveJSProperty('tagName', 'SPAN')

    // The regression guard: no form control can carry the date. `readonly`
    // would not be enough — Chrome's calendar picker ignores it — so nothing
    // input-shaped may exist in the editor at all.
    await expect(editor.locator('input')).toHaveCount(0)
    await expect(editor.locator('input[type="date"]')).toHaveCount(0)
    await expect(editor.locator('[contenteditable="true"]')).toHaveCount(0)
  })

  test('saving an edit sends the summary and no date', async ({ page }) => {
    const stub = await mockApi(page, [manualNote(NOTE_ID, NOTE_DATE, 'Original observation')])

    await openAliceNotes(page)
    await expect(page.getByTestId('notes-list')).toBeVisible({ timeout: 10000 })
    await page.getByTestId(`edit-note-${NOTE_ID}`).click()
    await expect(page.getByTestId('note-editor-textarea')).toBeVisible()

    await page.getByTestId('note-editor-textarea').fill('Updated observation')
    await page.getByTestId('note-editor-save').click()

    // The edit landed and the list refetched.
    await expect(page.getByTestId(`note-${NOTE_ID}`)).toContainText('Updated observation', { timeout: 10000 })

    expect(stub.putBodies).toHaveLength(1)
    const body = JSON.parse(stub.putBodies[0])
    expect(Object.keys(body)).toEqual(['summary'])
    expect(body).not.toHaveProperty('date')
    expect(body.summary).toBe('Updated observation')

    // The note keeps the date it was created with.
    await expect(page.getByTestId('notes-list')).toContainText(NOTE_DATE_LONG)
  })

  test('create mode still offers an editable date, and sends it', async ({ page }) => {
    const stub = await mockApi(page, [])

    await openAliceNotes(page)
    await expect(page.getByTestId('notes-empty')).toBeVisible({ timeout: 10000 })
    await page.getByTestId('add-note-btn').click()

    const dateInput = page.getByTestId('note-editor-date')
    await expect(dateInput).toBeVisible()
    await expect(dateInput).toHaveJSProperty('tagName', 'INPUT')
    await expect(dateInput).toHaveAttribute('type', 'date')
    // Stronger than checking for the absence of `readonly`: this also fails if
    // the control is disabled.
    await expect(dateInput).toBeEditable()
    // Counterpart to the edit-mode absence check: the same locator finds a date
    // input here, so a zero count there means the control is really gone.
    await expect(page.locator('.note-editor').locator('input[type="date"]')).toHaveCount(1)

    await dateInput.fill('2026-03-20')
    await page.getByTestId('note-editor-textarea').fill('Brand new observation')
    await page.getByTestId('note-editor-save').click()

    await expect(page.getByTestId('notes-list')).toContainText('Brand new observation', { timeout: 10000 })

    expect(stub.postBodies).toHaveLength(1)
    const body = JSON.parse(stub.postBodies[0])
    expect(body.date).toBe('2026-03-20')
    expect(body.summary).toBe('Brand new observation')
  })
})
