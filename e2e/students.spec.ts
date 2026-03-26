import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })

  // Mock GET /setup to indicate setup is already done, so the app skips DriveSetup
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
})

test.describe('Student list', () => {
  test('student list loads and displays grouped by class', async ({ page }) => {
    await page.route('**/students', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            spreadsheetUrl: 'https://docs.google.com/spreadsheets/d/abc123/edit',
            classes: [
              { name: '5A', students: [{ name: 'Emma Johnson' }, { name: 'Liam Smith' }] },
              { name: '5B', students: [{ name: 'Noah Davis' }, { name: 'Olivia Brown' }] },
            ],
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list')).toBeVisible({ timeout: 10000 })

    await expect(page.getByTestId('class-group-5A')).toBeVisible()
    await expect(page.getByTestId('class-group-5B')).toBeVisible()
    await expect(page.getByTestId('class-count-5A')).toHaveText('(2)')
    await expect(page.getByTestId('class-count-5B')).toHaveText('(2)')
    await expect(page.getByTestId('student-5A-Emma Johnson')).toBeVisible()
    await expect(page.getByTestId('student-5A-Liam Smith')).toBeVisible()
    await expect(page.getByTestId('student-5B-Noah Davis')).toBeVisible()
    await expect(page.getByTestId('student-5B-Olivia Brown')).toBeVisible()
  })

  test('empty spreadsheet shows info message and spreadsheet link', async ({ page }) => {
    await page.route('**/students', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 422,
          contentType: 'application/json',
          body: JSON.stringify({
            error: 'empty_spreadsheet',
            message: 'No students found. Add your students to the ClassSetup spreadsheet.',
            spreadsheetUrl: 'https://docs.google.com/spreadsheets/d/abc123/edit',
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list-empty')).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('heading', { name: 'No Students Found' })).toBeVisible()
    await expect(page.getByTestId('spreadsheet-link')).toBeVisible()
    await expect(page.getByTestId('spreadsheet-link')).toHaveAttribute(
      'href',
      'https://docs.google.com/spreadsheets/d/abc123/edit',
    )
  })

  test('spreadsheet not found shows error message', async ({ page }) => {
    await page.route('**/students', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            error: 'no_spreadsheet',
            message: 'ClassSetup spreadsheet not found. Try running setup again.',
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('student-list-no-spreadsheet')).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Setup Required')).toBeVisible()
    await expect(page.getByTestId('run-setup-again-btn')).toBeVisible()
  })

  test('refresh button re-fetches data', async ({ page }) => {
    await page.route('**/students', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 422,
          contentType: 'application/json',
          body: JSON.stringify({
            error: 'empty_spreadsheet',
            message: 'No students found.',
            spreadsheetUrl: 'https://docs.google.com/spreadsheets/d/abc123/edit',
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    await expect(page.getByTestId('student-list-empty')).toBeVisible({ timeout: 15000 })
    await expect(page.getByTestId('student-list-refresh')).toBeVisible()
    await expect(page.getByTestId('student-list-refresh')).toHaveText('Refresh')
  })
})
