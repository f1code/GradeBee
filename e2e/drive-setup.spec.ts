import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
})

test.describe('Drive setup flow', () => {
  test('shows the setup UI when signed in', async ({ page }) => {
    await page.goto('/')

    // Storage state from global setup should make us appear signed in.
    await expect(page.getByTestId('drive-setup')).toBeVisible({ timeout: 15000 })
    await expect(page.getByRole('heading', { name: 'Connect Google Drive' })).toBeVisible()
    await expect(page.getByTestId('setup-button')).toBeVisible()
    await expect(page.getByTestId('setup-button')).toHaveText('Set Up Google Drive')
  })

  test('shows loading state then success on setup', async ({ page }) => {
    // Mock the /setup API to return a success response
    await page.route('**/setup', async (route) => {
      // Small delay to allow the loading state to appear
      await new Promise((r) => setTimeout(r, 200))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          folderId: 'test-folder-id',
          folderUrl: 'https://drive.google.com/drive/folders/test-folder-id',
        }),
      })
    })

    await page.goto('/')
    await expect(page.getByTestId('drive-setup')).toBeVisible({ timeout: 15000 })

    const setupButton = page.getByTestId('setup-button')
    await setupButton.click()

    // Button should show loading state
    await expect(setupButton).toHaveText('Setting up...')
    await expect(setupButton).toBeDisabled()

    // After the mocked response, success state should appear
    await expect(page.getByTestId('drive-setup-success')).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('heading', { name: 'Google Drive Connected' })).toBeVisible()
    await expect(page.getByText('Your GradeBee folders are ready.')).toBeVisible()

    const driveLink = page.getByTestId('drive-link')
    await expect(driveLink).toBeVisible()
    await expect(driveLink).toHaveAttribute(
      'href',
      'https://drive.google.com/drive/folders/test-folder-id',
    )
    await expect(driveLink).toHaveAttribute('target', '_blank')
  })

  test('shows error state on setup failure and allows retry', async ({ page }) => {
    let callCount = 0

    await page.route('**/setup', async (route) => {
      callCount++
      if (callCount === 1) {
        // First call fails
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'Drive API unavailable' }),
        })
      } else {
        // Retry succeeds
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            folderId: 'test-folder-id',
            folderUrl: 'https://drive.google.com/drive/folders/test-folder-id',
          }),
        })
      }
    })

    await page.goto('/')
    await expect(page.getByTestId('drive-setup')).toBeVisible({ timeout: 15000 })

    // Click setup -- should fail
    await page.getByTestId('setup-button').click()

    // Error message should appear
    await expect(page.getByTestId('setup-error')).toBeVisible({ timeout: 10000 })

    // Button should be re-enabled for retry
    const setupButton = page.getByTestId('setup-button')
    await expect(setupButton).toBeEnabled()
    await expect(setupButton).toHaveText('Set Up Google Drive')

    // Retry -- should succeed
    await setupButton.click()
    await expect(page.getByTestId('drive-setup-success')).toBeVisible({ timeout: 10000 })
  })
})
