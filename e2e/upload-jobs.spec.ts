import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, Page } from '@playwright/test'

async function mockAuthenticatedApp(page: Page) {
  await page.route('**/setup', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ setupDone: true }),
      })
    } else {
      await route.continue()
    }
  })
  await page.route('**/students', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          spreadsheetUrl: 'https://docs.google.com/spreadsheets/d/abc/edit',
          classes: [{ name: '5A', students: [{ name: 'Emma' }] }],
        }),
      })
    } else {
      await route.continue()
    }
  })
}

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
  await mockAuthenticatedApp(page)
})

test.describe('Upload and job processing', () => {
  test('upload success shows toast and triggers job polling', async ({ page }) => {
    // Mock POST /upload
    await page.route('**/upload', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ fileId: 'f1', fileName: 'recording.mp3' }),
        })
      } else {
        await route.continue()
      }
    })

    // Mock GET /jobs with sequential responses
    let jobsCallCount = 0
    await page.route('**/jobs', async (route) => {
      if (route.request().method() === 'GET' && !route.request().url().includes('/jobs/')) {
        jobsCallCount++
        if (jobsCallCount <= 3) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              active: [{ fileId: 'f1', fileName: 'recording.mp3', status: 'transcribing' }],
              failed: [],
              done: [],
            }),
          })
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              active: [],
              failed: [],
              done: [
                {
                  fileId: 'f1',
                  fileName: 'recording.mp3',
                  status: 'done',
                  noteLinks: [{ name: 'Student', url: 'https://docs.google.com/document/d/abc/edit' }],
                },
              ],
            }),
          })
        }
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('audio-upload')).toBeVisible({ timeout: 10000 })

    // Upload a file
    await page.getByTestId('file-input').first().setInputFiles({
      name: 'recording.mp3',
      mimeType: 'audio/mpeg',
      buffer: Buffer.from('fake-audio'),
    })

    // Success toast appears (upload-progress may flash too quickly with instant mock)
    await expect(page.getByTestId('upload-success')).toBeVisible({ timeout: 5000 })
    await expect(page.getByTestId('upload-success')).toContainText('Uploaded! Processing in background.')

    // Active job appears
    await expect(page.getByTestId('job-active')).toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('job-active')).toContainText('recording.mp3')

    // Eventually transitions to done
    await expect(page.getByTestId('job-done')).toBeVisible({ timeout: 15000 })
    await expect(page.getByTestId('job-done')).toContainText('1 note created')
    await expect(page.getByTestId('job-done').locator('a.job-note-link')).toBeVisible()
  })

  test('upload error shows error state and retry', async ({ page }) => {
    // Mock POST /upload to fail
    await page.route('**/upload', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'Drive API unavailable' }),
        })
      } else {
        await route.continue()
      }
    })

    // Mock GET /jobs empty
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

    await page.goto('/')
    await expect(page.getByTestId('audio-upload')).toBeVisible({ timeout: 10000 })

    // Upload a file
    await page.getByTestId('file-input').first().setInputFiles({
      name: 'bad.mp3',
      mimeType: 'audio/mpeg',
      buffer: Buffer.from('fake-audio'),
    })

    // Error state appears
    await expect(page.getByTestId('upload-error')).toBeVisible({ timeout: 5000 })

    // Click "Try again"
    await page.getByTestId('upload-error').getByRole('button', { name: 'Try again' }).click()

    // Back to idle — drop zone or mobile upload reappears
    await expect(
      page.getByTestId('drop-zone').or(page.getByTestId('mobile-upload')),
    ).toBeVisible({ timeout: 5000 })
  })

  test('job status shows active jobs with progress labels', async ({ page }) => {
    await page.route('**/jobs', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            active: [
              { fileId: 'f1', fileName: 'a.mp3', status: 'queued' },
              { fileId: 'f2', fileName: 'b.mp3', status: 'transcribing' },
              { fileId: 'f3', fileName: 'c.mp3', status: 'extracting' },
              { fileId: 'f4', fileName: 'd.mp3', status: 'creating_notes' },
            ],
            failed: [],
            done: [],
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('job-status')).toBeVisible({ timeout: 10000 })

    const activeJobs = page.getByTestId('job-active')
    await expect(activeJobs).toHaveCount(4)

    await expect(activeJobs.nth(0)).toContainText('Queued')
    await expect(activeJobs.nth(1)).toContainText('Transcribing')
    await expect(activeJobs.nth(2)).toContainText('Analyzing transcript')
    await expect(activeJobs.nth(3)).toContainText('Creating notes')
  })

  test('job status shows failed jobs with retry', async ({ page }) => {
    let jobsCallCount = 0

    await page.route('**/jobs', async (route) => {
      if (route.request().method() === 'GET' && !route.request().url().includes('/jobs/')) {
        jobsCallCount++
        if (jobsCallCount <= 2) {
          // Initial state: one failed job
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              active: [],
              failed: [
                {
                  fileId: 'f1',
                  fileName: 'bad.mp3',
                  status: 'failed',
                  error: 'Whisper timeout',
                },
              ],
              done: [],
            }),
          })
        } else {
          // After retry: job moved to active
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              active: [{ fileId: 'f1', fileName: 'bad.mp3', status: 'queued' }],
              failed: [],
              done: [],
            }),
          })
        }
      } else {
        await route.continue()
      }
    })

    // Mock POST /jobs/retry
    await page.route('**/jobs/retry', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ok: true }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    // Failed section visible
    await expect(page.getByTestId('job-failed-section')).toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('job-failed')).toContainText('bad.mp3')
    await expect(page.getByTestId('job-failed')).toContainText('Whisper timeout')

    // Retry button visible
    const retryBtn = page.getByTestId('job-retry-btn')
    await expect(retryBtn).toBeVisible()
    await expect(retryBtn).toHaveText('Retry All')

    // Click retry
    await retryBtn.click()

    // Failed section disappears, active appears
    await expect(page.getByTestId('job-failed-section')).not.toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('job-active')).toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('job-active')).toContainText('bad.mp3')
  })

  test('done jobs show note links and new badge', async ({ page }) => {
    await page.route('**/jobs', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            active: [],
            failed: [],
            done: [
              {
                fileId: 'f1',
                fileName: 'lesson.mp3',
                status: 'done',
                noteLinks: [
                  { name: 'Alice', url: 'https://docs.google.com/doc1' },
                  { name: 'Bob', url: 'https://docs.google.com/doc2' },
                ],
              },
            ],
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    const doneCard = page.getByTestId('job-done')
    await expect(doneCard).toBeVisible({ timeout: 10000 })
    await expect(doneCard).toContainText('2 notes created')

    // Two "Open note" links
    const noteLinks = doneCard.locator('a.job-note-link')
    await expect(noteLinks).toHaveCount(2)
    await expect(noteLinks.nth(0)).toHaveAttribute('href', 'https://docs.google.com/doc1')
    await expect(noteLinks.nth(0)).toHaveAttribute('target', '_blank')
    await expect(noteLinks.nth(1)).toHaveAttribute('href', 'https://docs.google.com/doc2')
    await expect(noteLinks.nth(1)).toHaveAttribute('target', '_blank')

    // New badge visible
    const badge = page.getByTestId('job-new-badge')
    await expect(badge).toBeVisible()

    // Click dismisses the badge
    await badge.click()
    await expect(badge).not.toBeVisible({ timeout: 3000 })
  })

  test('empty job list renders nothing', async ({ page }) => {
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

    await page.goto('/')

    // Wait for the page to load (student list should be visible)
    await expect(page.getByTestId('student-list')).toBeVisible({ timeout: 10000 })

    // Job status should not be visible
    await expect(page.getByTestId('job-status')).not.toBeVisible()
  })

  test('job polling error shows error message', async ({ page }) => {
    // First call succeeds with an active job, subsequent calls fail.
    // This ensures `jobs` is non-null so the error div can render.
    let jobsErrorCallCount = 0
    await page.route('**/jobs', async (route) => {
      if (route.request().method() === 'GET') {
        jobsErrorCallCount++
        if (jobsErrorCallCount <= 1) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              active: [{ fileId: 'f1', fileName: 'test.mp3', status: 'transcribing' }],
              failed: [],
              done: [],
            }),
          })
        } else {
          await route.fulfill({
            status: 500,
            contentType: 'application/json',
            body: JSON.stringify({ error: 'queue unavailable' }),
          })
        }
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    // Active job appears first
    await expect(page.getByTestId('job-active')).toBeVisible({ timeout: 10000 })

    // After next poll fails, error message appears
    await expect(page.getByTestId('job-error')).toBeVisible({ timeout: 15000 })
    await expect(page.getByTestId('job-error')).toContainText('queue unavailable')
  })
})
