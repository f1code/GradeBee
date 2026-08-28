import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, Page } from '@playwright/test'

/**
 * Move-to-class e2e coverage. Like the other authenticated specs, the backend
 * is stubbed at the network edge (see students.spec.ts / reports.spec.ts) so
 * the run never depends on — or pollutes — the dev sqlite db. The stub keeps a
 * small mutable roster so a PUT /students/{id} is observable through the
 * subsequent GET /classes/{id}/students refetch, which is what the UI does.
 */

type Student = { id: number; name: string; aliases?: string[] }

const CLASSES = [
  { id: 1, levelId: 1, name: '5A · Mon', levelName: '5A', day: 'Monday', timeSlot: '' },
  { id: 2, levelId: 1, name: '5A · Wed', levelName: '5A', day: 'Wednesday', timeSlot: '' },
  { id: 3, levelId: 2, name: '5B · Mon', levelName: '5B', day: 'Monday', timeSlot: '' },
]

const ALICE = 101

function initialRoster(): Map<number, Student[]> {
  return new Map<number, Student[]>([
    [1, [{ id: ALICE, name: 'Alice' }, { id: 102, name: 'Bob' }]],
    [2, [{ id: 201, name: 'Carol' }]],
    [3, [{ id: 301, name: 'Dave' }]],
  ])
}

interface MoveOutcome {
  /** 409 conflict: the student already occupying that name in the target. */
  conflictWith?: string
  /** Aliases silently dropped because the target class already used them. */
  droppedAliases?: string[]
}

async function mockApi(page: Page, outcome: MoveOutcome = {}) {
  const roster = initialRoster()

  await page.route('**/classes', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          classes: CLASSES.map((c) => ({ ...c, studentCount: (roster.get(c.id) || []).length })),
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
        body: JSON.stringify({
          levels: [
            { id: 1, name: '5A', reportInstructions: '' },
            { id: 2, name: '5B', reportInstructions: '' },
          ],
        }),
      })
    } else {
      await route.continue()
    }
  })

  await page.route('**/classes/*/students', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    const classId = Number(new URL(route.request().url()).pathname.split('/').at(-2))
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ students: roster.get(classId) || [] }),
    })
  })

  // PUT /students/{id} with {classId} is the move; reject or relocate.
  await page.route('**/students/*', async (route) => {
    const request = route.request()
    if (request.method() !== 'PUT') {
      await route.continue()
      return
    }
    const body = request.postDataJSON() as { classId?: number }
    if (typeof body?.classId !== 'number') {
      await route.continue()
      return
    }

    if (outcome.conflictWith) {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'name conflict',
          message: 'A student with that name already exists in the target class',
          details: { conflictStudentName: outcome.conflictWith },
        }),
      })
      return
    }

    const studentId = Number(new URL(request.url()).pathname.split('/').at(-1))
    let moved: Student | undefined
    for (const [classId, students] of roster) {
      const found = students.find((s) => s.id === studentId)
      if (found) {
        moved = found
        roster.set(classId, students.filter((s) => s.id !== studentId))
        break
      }
    }
    if (moved) {
      const dropped = new Set(outcome.droppedAliases || [])
      const kept = (moved.aliases || []).filter((a) => !dropped.has(a))
      const target = roster.get(body.classId) || []
      roster.set(
        body.classId,
        [...target, { ...moved, aliases: kept }].sort((a, b) => a.name.localeCompare(b.name))
      )
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ droppedAliases: outcome.droppedAliases || [] }),
    })
  })

  // Student detail panel fetches; keep them empty and quiet.
  await page.route('**/students/*/notes', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notes: [] }) })
  })
  await page.route('**/students/*/aliases', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ aliases: [] }) })
    } else {
      await route.continue()
    }
  })
  // Empty job lists so the job-status poller stays out of the way.
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
}

/** Expand class 1, open Alice's detail, and click the move trigger. */
async function openMovePicker(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('student-list')).toBeVisible({ timeout: 10000 })

  await page.getByTestId('class-toggle-1').click()
  await expect(page.getByTestId(`student-${ALICE}`)).toBeVisible()

  await page.getByTestId(`student-${ALICE}`).getByText('Alice').click()
  await expect(page.getByTestId(`move-student-${ALICE}`)).toBeVisible()

  await page.getByTestId(`move-student-${ALICE}`).click()
  await expect(page.getByTestId('move-student-overlay')).toBeVisible()
  await expect(page.getByTestId('move-student-pick')).toBeVisible()
}

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
})

test.describe('Move student to another class', () => {
  test('same-Level target moves immediately and updates both class counts', async ({ page }) => {
    await mockApi(page)
    await openMovePicker(page)

    // The current class is not offered as a target.
    await expect(page.getByTestId('move-student-target-1')).toHaveCount(0)
    await expect(page.getByTestId('move-student-target-2')).toBeVisible()
    await expect(page.getByTestId('move-student-target-3')).toBeVisible()

    // Same Level (5A) => no confirm step, straight to the result.
    await page.getByTestId('move-student-target-2').click()
    await expect(page.getByTestId('move-student-result')).toBeVisible()
    await expect(page.getByTestId('move-student-confirm')).toHaveCount(0)
    await page.getByTestId('move-student-done-btn').click()
    await expect(page.getByTestId('move-student-overlay')).toHaveCount(0)

    // Gone from the source class, counts adjusted on both sides.
    await expect(page.getByTestId(`student-${ALICE}`)).toHaveCount(0)
    await expect(page.getByTestId('class-group-1').getByText('(1)')).toBeVisible()
    await expect(page.getByTestId('class-group-2').getByText('(2)')).toBeVisible()

    // And present in the target class once expanded.
    await page.getByTestId('class-toggle-2').click()
    await expect(page.getByTestId('class-group-2').getByTestId(`student-${ALICE}`)).toBeVisible()
  })

  test('cross-Level target requires confirming a Report Instructions warning', async ({ page }) => {
    await mockApi(page)
    await openMovePicker(page)

    // Different Level (5B) => confirm step, nothing moved yet.
    await page.getByTestId('move-student-target-3').click()
    const confirm = page.getByTestId('move-student-confirm')
    await expect(confirm).toBeVisible()
    await expect(confirm).toContainText('Report Instructions')
    await expect(confirm).toContainText('5B')
    await expect(page.getByTestId('class-group-1').getByText('(2)')).toBeVisible()

    await page.getByTestId('move-student-confirm-btn').click()
    await expect(page.getByTestId('move-student-result')).toBeVisible()
    await page.getByTestId('move-student-done-btn').click()

    await expect(page.getByTestId(`student-${ALICE}`)).toHaveCount(0)
    await expect(page.getByTestId('class-group-1').getByText('(1)')).toBeVisible()
    await expect(page.getByTestId('class-group-3').getByText('(2)')).toBeVisible()
  })

  test('name conflict in the target class reports the collision and moves nothing', async ({ page }) => {
    await mockApi(page, { conflictWith: 'Alice' })
    await openMovePicker(page)

    await page.getByTestId('move-student-target-2').click()

    // Stays on the picker with an inline conflict naming the other student.
    await expect(page.getByTestId('move-student-pick')).toBeVisible()
    await expect(page.getByTestId('move-student-result')).toHaveCount(0)
    await expect(page.getByTestId('move-student-pick')).toContainText('Alice')
    await expect(page.getByTestId('move-student-pick')).toContainText(/already has a student with this name/i)

    // Nothing moved: counts unchanged after dismissing.
    await page.getByRole('button', { name: 'Close' }).click()
    await expect(page.getByTestId('move-student-overlay')).toHaveCount(0)
    await expect(page.getByTestId(`student-${ALICE}`)).toBeVisible()
    await expect(page.getByTestId('class-group-1').getByText('(2)')).toBeVisible()
    await expect(page.getByTestId('class-group-2').getByText('(1)')).toBeVisible()
  })

  test('aliases colliding with the target class are dropped and reported', async ({ page }) => {
    await mockApi(page, { droppedAliases: ['Ali'] })
    await openMovePicker(page)

    await page.getByTestId('move-student-target-2').click()

    const result = page.getByTestId('move-student-result')
    await expect(result).toBeVisible()
    await expect(result).toContainText('Ali')
    await expect(result).toContainText(/dropped alias/i)

    // Dropping an alias does not block the move.
    await page.getByTestId('move-student-done-btn').click()
    await expect(page.getByTestId(`student-${ALICE}`)).toHaveCount(0)
    await expect(page.getByTestId('class-group-2').getByText('(2)')).toBeVisible()
  })

  test('Escape dismisses the modal but an overlay click does not', async ({ page }) => {
    await mockApi(page)
    await openMovePicker(page)

    // A stray tap on the backdrop must not abandon the move.
    await page.mouse.click(5, 5)
    await expect(page.getByTestId('move-student-overlay')).toBeVisible()
    await expect(page.getByTestId('move-student-pick')).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByTestId('move-student-overlay')).toHaveCount(0)

    // Nothing moved.
    await expect(page.getByTestId(`student-${ALICE}`)).toBeVisible()
    await expect(page.getByTestId('class-group-1').getByText('(2)')).toBeVisible()
  })
})
