import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, type Page } from '@playwright/test'

/**
 * The class picker in a browser: a recording whose class was never pinned, the
 * teacher picking one, and the card repainting with the notes it made (#127).
 *
 * `/assemble` is mocked. Its own contract — ownership, the second extraction
 * pass, the lock, the job update — is pinned by the Go handler tests against
 * the real router; what only a browser can show is that the card puts the
 * picker up on this reason, sends the class and nothing else, holds the options
 * dead for the round trip, and takes the picker down when the notes exist.
 */

// The pair of sibling classes a teacher can pick the wrong one of.
async function mockClasses(page: Page) {
  await page.route('**/classes', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          classes: [
            { id: 1, name: 'Pam & Paul · Wed · 14.10', day: 'Wednesday', timeSlot: '14.10', studentCount: 6 },
            { id: 2, name: 'Pam & Paul · Wed · 16.30', day: 'Wednesday', timeSlot: '16.30', studentCount: 6 },
          ],
        }),
      })
    } else {
      await route.continue()
    }
  })
}

// A declined recording: done, no notes, no passages, and the reason that says
// the extraction could not place it.
async function mockDeclinedJob(page: Page) {
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
            fileName: 'wednesday.m4a',
            status: 'done',
            noteLinks: [],
            passages: [],
            noNotesReason: 'class_unclear',
            canPickClass: true,
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
  await mockDeclinedJob(page)
})

test.describe('Class picker', () => {
  test('a declined recording is filed by picking its class', async ({ page }) => {
    // Released once the assertions about the in-flight state have run, so the
    // 2-3s the real endpoint takes is under the test's control rather than a
    // sleep.
    let release: () => void = () => {}
    const inFlight = new Promise<void>((resolve) => { release = resolve })
    const bodies: unknown[] = []

    await page.route('**/assemble', async (route) => {
      bodies.push(route.request().postDataJSON())
      await inFlight
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          className: 'Pam & Paul · Wed · 16.30',
          noteLinks: [{ name: 'Ombeline', noteId: 40, studentId: 11, className: 'Pam & Paul · Wed · 16.30' }],
          passages: [{ kind: 'child', spokenLabels: ['Ombeline'], student: 'Ombeline', summary: 'read well' }],
        }),
      })
    })

    await page.goto('/')

    const card = page.getByTestId('job-done')
    await expect(card).toBeVisible({ timeout: 15000 })
    await expect(card).toContainText("the class wasn't clear")

    // Scoped to the picker throughout: the class-management list on the same
    // page carries every class name again, on its rename and delete buttons.
    const picker = page.getByTestId('class-picker')
    await expect(picker).toBeVisible()
    await expect(picker.getByTestId('class-picker-option')).toHaveCount(2)

    await picker.getByRole('button', { name: 'Pam & Paul · Wed · 16.30' }).click()

    // Dead for the whole round trip: an impatient teacher must not be able to
    // file the same recording under a second class.
    await expect(picker.getByRole('button', { name: 'Creating notes…' })).toBeVisible()
    for (const option of await picker.getByTestId('class-picker-option').all()) {
      await expect(option).toBeDisabled()
    }

    release()

    await expect(card).toContainText('1 note created')
    await expect(picker).toBeHidden()
    await expect(card).toContainText('Ombeline')

    // The class is the whole body. The card's passages stay on the card: since
    // #127 the server writes every note from its own extraction pass, so a
    // caller cannot put words behind the model's name.
    expect(bodies).toEqual([{ className: 'Pam & Paul · Wed · 16.30' }])
  })

  test('a pick that resolves nobody leaves the picker up for another try', async ({ page }) => {
    // The server reports the pick and files nothing: the picked class and the
    // run's passages, with the card's own reason and the picker gate unchanged.
    await page.route('**/assemble', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          className: 'Pam & Paul · Wed · 14.10',
          classId: 1,
          noteLinks: [],
          passages: [],
          noNotesReason: 'class_unclear',
          canPickClass: true,
        }),
      })
    })

    await page.goto('/')
    await expect(page.getByTestId('job-done')).toBeVisible({ timeout: 15000 })

    const picker = page.getByTestId('class-picker')
    await expect(picker).toBeVisible()
    await picker.getByRole('button', { name: 'Pam & Paul · Wed · 14.10' }).click()

    // Picking the wrong one of two siblings is the mistake this path exists to
    // undo, so the second attempt has to be there.
    await expect(picker.getByRole('button', { name: 'Pam & Paul · Wed · 16.30' })).toBeEnabled()
    await expect(picker).toBeVisible()
  })
})
