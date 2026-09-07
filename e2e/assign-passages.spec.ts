import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, type Page } from '@playwright/test'

/**
 * Filing a passage that reached nobody to a child, in a browser (#134): a done
 * card with a pinned class lists the rows, the teacher ticks one, picks a
 * child of that class, confirms, and the card repaints with the note. A child
 * the card already links to gets the row appended to that note (#135).
 *
 * A wrong pick is undone from the row (#138): the note goes, the count and
 * the child's link with it, and the row is open again.
 *
 * `/assign` and `/classes/{id}/students` are mocked. The endpoint's own
 * contract — ownership, membership, validation, the lock, the job update — is
 * pinned by the Go handler tests against the real router; what only a browser
 * can show is that the card offers the roster of the class on the job, sends
 * the ticked rows plus the group passage and nothing else, holds the picker
 * dead for the round trip, and marks the row assigned when the note exists.
 */

async function mockClasses(page: Page) {
  await page.route('**/classes', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          classes: [{ id: 2, name: 'Pam & Paul · Tue', day: 'Tuesday', timeSlot: '', studentCount: 2 }],
        }),
      })
    } else {
      await route.continue()
    }
  })
}

async function mockRoster(page: Page) {
  await page.route('**/classes/2/students', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        students: [
          { id: 21, classId: 2, name: 'Lévy', createdAt: '', aliases: [] },
          { id: 22, classId: 2, name: 'Eleonore', createdAt: '', aliases: [] },
        ],
      }),
    })
  })
}

// Note 694's shape: a class pinned, one child filed, two blocks the recording
// never named anybody in, and a class-wide remark.
async function mockDoneJob(page: Page, classId: number | undefined) {
  await page.route('**/jobs', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/jobs/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          active: [],
          failed: [],
          done: [{
            uploadId: 7,
            fileName: 'tuesday.m4a',
            status: 'done',
            className: 'Pam & Paul · Tue',
            classId,
            noteLinks: [{ name: 'Lévy', noteId: 50, studentId: 21, className: 'Pam & Paul · Tue' }],
            passages: [
              { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
              { kind: 'unknown', summary: "Polly wasn't speaking much today." },
              { kind: 'child', spokenLabels: ['Lévy'], student: 'Lévy', summary: 'Lévy finished the puzzle alone.' },
              { kind: 'group', summary: 'Everyone worked hard.' },
            ],
          }],
        }),
      })
    } else {
      await route.continue()
    }
  })
}

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
  await mockClasses(page)
  await mockRoster(page)
})

test.describe('Filing passages to a child', () => {
  test('a ticked row is filed to the picked child as a new note', async ({ page }) => {
    await mockDoneJob(page, 2)

    // Released once the in-flight assertions have run, so the round trip is
    // under the test's control rather than a sleep.
    let release: () => void = () => {}
    const inFlight = new Promise<void>((resolve) => { release = resolve })
    const bodies: unknown[] = []

    await page.route('**/assign', async (route) => {
      bodies.push(route.request().postDataJSON())
      await inFlight
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Pam & Paul · Tue', appended: false }),
      })
    })

    await page.goto('/')

    const card = page.getByTestId('job-done')
    await expect(card).toBeVisible({ timeout: 15000 })
    await expect(card).toContainText('1 note created')

    const review = card.getByTestId('passage-review')
    await expect(review.getByTestId('passage-review-row')).toHaveCount(2)
    await expect(review.getByTestId('passage-review-check')).toHaveCount(2)

    // The pick is the confirm, so every chip is dead until a row is ticked.
    const eleonore = review.getByRole('button', { name: 'Eleonore', exact: true })
    await expect(eleonore).toBeDisabled()

    await review.getByTestId('passage-review-check').first().check()
    await expect(eleonore).toBeEnabled()
    await eleonore.click()

    // Dead for the round trip: a second pick must not file the row twice.
    await expect(eleonore).toBeDisabled()
    await expect(review.getByTestId('passage-review-prompt')).toHaveText('Assigning to Eleonore…')

    release()

    await expect(card).toContainText('2 notes created')
    await expect(review.getByTestId('passage-review-filed')).toHaveText(/Assigned to Eleonore/)
    await expect(review.getByTestId('passage-review-check').first()).toBeDisabled()
    await expect(review.getByTestId('passage-review-check').nth(1)).toBeEnabled()
    await expect(card.getByTestId('job-note-link').filter({ hasText: 'Eleonore' })).toBeVisible()

    // The ticked row, then the class-wide remark; the other row stays where
    // it is, and the job's own passages are not sent back.
    expect(bodies).toEqual([{
      classId: 2,
      studentId: 22,
      passages: [
        { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
        { kind: 'group', summary: 'Everyone worked hard.' },
      ],
    }])
  })

  // Lévy is already a link on the card, so picking Lévy sends that note's id
  // and the card gains no second link.
  test('a row filed to a child who has a note joins that note', async ({ page }) => {
    await mockDoneJob(page, 2)
    const bodies: unknown[] = []
    await page.route('**/assign', async (route) => {
      bodies.push(route.request().postDataJSON())
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ noteId: 50, studentId: 21, name: 'Lévy', className: 'Pam & Paul · Tue', appended: true }),
      })
    })

    await page.goto('/')
    const card = page.getByTestId('job-done')
    await expect(card).toBeVisible({ timeout: 15000 })
    const review = card.getByTestId('passage-review')
    await review.getByTestId('passage-review-check').first().check()
    await review.getByRole('button', { name: 'Lévy', exact: true }).click()

    await expect(review.getByTestId('passage-review-filed')).toHaveText('Assigned to Lévy')
    await expect(card).toContainText('1 note created')
    await expect(card.getByTestId('job-note-link').filter({ hasText: 'Lévy' })).toHaveCount(1)
    expect(bodies).toEqual([{
      classId: 2,
      studentId: 21,
      passages: [
        { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
        { kind: 'group', summary: 'Everyone worked hard.' },
      ],
      appendToNoteId: 50,
    }])
  })

  test('an assignment is undone from the row', async ({ page }) => {
    await mockDoneJob(page, 2)
    await page.route('**/assign', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Pam & Paul · Tue', appended: false }),
      })
    })
    const undos: string[] = []
    await page.route('**/assign/22', async (route) => {
      undos.push(route.request().method())
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ noteIds: [60] }),
      })
    })

    await page.goto('/')
    const card = page.getByTestId('job-done')
    await expect(card).toBeVisible({ timeout: 15000 })
    const review = card.getByTestId('passage-review')
    await review.getByTestId('passage-review-check').first().check()
    await review.getByRole('button', { name: 'Eleonore', exact: true }).click()
    await expect(card).toContainText('2 notes created')
    await expect(review.getByTestId('passage-review-filed')).toHaveText(/Assigned to Eleonore/)

    await review.getByTestId('passage-review-undo').click()

    await expect(card).toContainText('1 note created')
    await expect(review.getByTestId('passage-review-filed')).toHaveCount(0)
    await expect(review.getByTestId('passage-review-check').first()).toBeEnabled()
    await expect(review.getByTestId('passage-review-check').first()).not.toBeChecked()
    await expect(card.getByTestId('job-note-link').filter({ hasText: 'Eleonore' })).toHaveCount(0)
    expect(undos).toEqual(['DELETE'])
  })

  test('a card with no class id lists the rows read-only', async ({ page }) => {
    await mockDoneJob(page, undefined)

    await page.goto('/')
    const card = page.getByTestId('job-done')
    await expect(card).toBeVisible({ timeout: 15000 })

    const review = card.getByTestId('passage-review')
    await expect(review.getByTestId('passage-review-row')).toHaveCount(2)
    await expect(review.getByTestId('passage-review-check')).toHaveCount(0)
    await expect(review.getByTestId('passage-review-student')).toHaveCount(0)
  })
})
