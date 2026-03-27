import { setupClerkTestingToken } from '@clerk/testing/playwright'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await setupClerkTestingToken({ page })
})

test.describe('Signed-out experience', () => {
  test('shows the GradeBee heading', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'GradeBee', level: 1 })).toBeVisible()
  })

  test('shows welcome message and sign-in button', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByTestId('sign-in-container')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Welcome to GradeBee' })).toBeVisible()
    await expect(page.getByText(/Record verbal feedback about your students/)).toBeVisible()
    await expect(page.getByTestId('sign-in-button')).toBeVisible()
    await expect(page.getByTestId('sign-in-button')).toBeEnabled()
  })

  test('does not show authenticated UI when signed out', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByTestId('audio-upload')).not.toBeVisible()
    await expect(page.getByTestId('student-list')).not.toBeVisible()
  })
})
