import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect, Page } from '@playwright/test'

async function mockClassesAndStudents(page: Page) {
  await page.route('**/classes', async (route) => {
    if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          classes: [{ id: 1, name: 'Science', studentCount: 1 }],
        }),
      })
    } else {
      await route.continue()
    }
  })
  await page.route('**/classes/1/students', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        students: [{ id: 10, classId: 1, name: 'Alice', createdAt: '2026-01-01T00:00:00Z' }],
      }),
    })
  })
  // Empty jobs so job status doesn't interfere
  await page.route('**/jobs', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ active: [], failed: [], done: [] }),
    })
  })
  // Empty examples
  await page.route('**/report-examples', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ examples: [] }),
      })
    } else {
      await route.continue()
    }
  })
}

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
  await mockClassesAndStudents(page)
})

test.describe('Report generation', () => {
  test('generate report shows result with correct fields', async ({ page }) => {
    await page.route('**/reports', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            reports: [
              {
                id: 42,
                studentId: 10,
                student: 'Alice',
                className: 'Science',
                html: '<p>Alice shows great progress in science.</p>',
                startDate: '2026-01-01',
                endDate: '2026-03-31',
                createdAt: '2026-04-03T12:00:00Z',
              },
            ],
            error: null,
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    // Navigate to reports tab
    await page.getByText('Reports').click()

    // Wait for student list to load in report generation
    await expect(page.getByText('Alice')).toBeVisible({ timeout: 10000 })

    // Select the class (all students in it)
    await page.getByText('Science').click()

    // Click generate
    await page.getByRole('button', { name: /Generate.*Report/ }).click()

    // Report result appears
    await expect(page.getByText('Generated Reports')).toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('report-result-name')).toBeVisible({ timeout: 5000 })
    await expect(page.getByTestId('report-result-name')).toContainText('Alice')
  })

  test('class with schedule name matches example by base levelName', async ({ page }) => {
    // Regression test: c.name is "Math — Schedule A" but examples store just "Math"
    // The matching must use c.levelName, not c.name.
    await page.route('**/classes', async (route) => {
      if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            classes: [{ id: 1, name: 'Math — Group A', levelName: 'Math', scheduleName: 'Group A', studentCount: 1 }],
          }),
        })
      } else {
        await route.continue()
      }
    })
    await page.route('**/report-examples', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            examples: [{ id: 1, name: 'Math example.pdf', content: 'Example content.', status: 'ready', levelNames: ['Math'] }],
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await page.getByText('Reports').click()
    await expect(page.getByText('Alice')).toBeVisible({ timeout: 10000 })

    // Select Alice (whose class has a schedule suffix in its display name)
    await page.getByText('Alice').click()

    // The example's levelNames: ['Math'] should match c.levelName 'Math', not c.name 'Math — Group A'
    await expect(page.getByTestId('generate-blocker')).not.toBeAttached()
    await expect(page.getByRole('button', { name: /Generate.*Report/ })).toBeEnabled()
  })

  test('thumbs-down on generated report captures feedback', async ({ page }) => {
    // Mock report generation
    await page.route('**/reports', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            reports: [{
              id: 42,
              studentId: 10,
              student: 'Alice',
              className: 'Science',
              html: '<p>Alice shows great progress in science.</p>',
              startDate: '2026-01-01',
              endDate: '2026-03-31',
              createdAt: '2026-04-03T12:00:00Z',
            }],
            error: null,
          }),
        })
      } else {
        await route.continue()
      }
    })

    // Capture the feedback POST request
    const feedbackBodies: string[] = []
    await page.route('**/feedback', async (route) => {
      if (route.request().method() === 'POST') {
        feedbackBodies.push(route.request().postData() ?? '')
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ id: 1, created_at: '2026-04-03T12:00:00Z' }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await page.getByText('Reports').click()
    await expect(page.getByText('Alice')).toBeVisible({ timeout: 10_000 })
    await page.getByText('Science').click()
    await page.getByRole('button', { name: /Generate.*Report/ }).click()
    await expect(page.getByTestId('report-result-name')).toBeVisible({ timeout: 10_000 })

    // Expand the report by clicking the result row
    await page.getByTestId('report-result-name').click()

    // Wait for the thumbs buttons inside ReportViewer
    await expect(page.getByTestId('thumb-down')).toBeVisible({ timeout: 5_000 })

    // Click thumbs-down
    await page.getByTestId('thumb-down').click()

    // Comment textarea appears
    await expect(page.getByTestId('thumb-down-comment')).toBeVisible({ timeout: 3_000 })

    // Fill in a comment and submit
    await page.getByTestId('thumb-down-comment').fill('Too short, lacks detail')
    await page.getByTestId('thumb-down-submit').click()

    // Confirmation message appears
    await expect(page.getByText(/thanks for your feedback/i)).toBeVisible({ timeout: 5_000 })

    // Verify the API call was made with the correct payload
    expect(feedbackBodies.length).toBe(1)
    const payload = JSON.parse(feedbackBodies[0])
    expect(payload.artifact_type).toBe('report')
    expect(payload.artifact_id).toBe(42)
    expect(payload.rating).toBe('down')
    expect(payload.comment).toBe('Too short, lacks detail')
  })
})

