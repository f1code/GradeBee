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
              { id: 1, levelId: 1, name: '5A · Mon', levelName: '5A', day: 'Monday', timeSlot: '', studentCount: 2 },
              { id: 2, levelId: 2, name: '5B · Mon', levelName: '5B', day: 'Monday', timeSlot: '', studentCount: 2 },
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

  test('add class form: selecting a day enables submit and creates the class', async ({ page }) => {
    await page.route('**/classes', async (route) => {
      const req = route.request()
      if (req.method() === 'GET' && !req.url().includes('/classes/')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ classes: [] }),
        })
      } else if (req.method() === 'POST') {
        const body = req.postDataJSON()
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({
            id: 1,
            levelId: body.levelId,
            levelName: '5A',
            name: '5A · ' + body.day.slice(0, 3),
            day: body.day,
            timeSlot: body.timeSlot,
            studentCount: 0,
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
          body: JSON.stringify({ levels: [{ id: 1, name: '5A', reportInstructions: 'x' }] }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list-empty')).toBeVisible({ timeout: 10000 })

    const submit = page.getByTestId('add-class-submit')
    await expect(submit).toBeDisabled()

    await page.getByTestId('add-class-level-select').selectOption('1')
    await expect(submit).toBeDisabled()

    await page.getByTestId('add-class-day-select').selectOption('Wednesday')
    await expect(submit).toBeEnabled()

    await submit.click()

    await expect(page.getByTestId('class-group-1')).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('5A')).toBeVisible()
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
