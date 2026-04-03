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
                class: 'Science',
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
})
