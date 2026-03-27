import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
})

test.describe('Student list', () => {
  test('class list loads and shows class groups', async ({ page }) => {
    await page.route('**/classes', async (route) => {
      if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            classes: [
              { id: 1, name: '5A', studentCount: 2 },
              { id: 2, name: '5B', studentCount: 2 },
            ],
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list')).toBeVisible({ timeout: 10000 })

    await expect(page.getByTestId('class-group-1')).toBeVisible()
    await expect(page.getByTestId('class-group-2')).toBeVisible()
    await expect(page.getByText('5A')).toBeVisible()
    await expect(page.getByText('5B')).toBeVisible()
  })

  test('no classes shows empty state with add class form', async ({ page }) => {
    await page.route('**/classes', async (route) => {
      if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ classes: [] }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list-empty')).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('heading', { name: /no classes/i })).toBeVisible()
  })

  test('error state shows retry button', async ({ page }) => {
    await page.route('**/classes', async (route) => {
      if (route.request().method() === 'GET' && !route.request().url().includes('/classes/')) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'internal error' }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list-error')).toBeVisible({ timeout: 10000 })
    await expect(page.getByTestId('student-list-refresh')).toBeVisible()
  })
})
