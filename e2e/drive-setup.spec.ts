import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
})

test.describe('Authenticated app loads correctly', () => {
  test('shows the main UI when signed in', async ({ page }) => {
    await page.goto('/')

    // Storage state from global setup should make us appear signed in.
    await expect(page.getByTestId('audio-upload')).toBeVisible({ timeout: 15000 })
    await expect(page.getByRole('heading', { name: 'Add Notes' })).toBeVisible()
  })

  test('shows class management UI', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByTestId('audio-upload')).toBeVisible({ timeout: 15000 })

    // The add class form should be available
    await expect(page.getByTestId('add-class-btn').or(page.getByTestId('add-class-input'))).toBeVisible()
  })
})
