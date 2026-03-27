import { clerk, clerkSetup } from '@clerk/testing/playwright'
import { test as setup, expect } from '@playwright/test'
import path from 'path'
import fs from 'fs'

setup.describe.configure({ mode: 'serial' })

const authFile = path.join(__dirname, '../playwright/.clerk/user.json')

const TEST_EMAIL = 'gradebee+clerk_test@example.com'

setup('global setup', async () => {
  await clerkSetup()
})

setup('authenticate', async ({ page }) => {
  fs.mkdirSync(path.dirname(authFile), { recursive: true })

  await page.goto('/')
  await clerk.signIn({
    page,
    emailAddress: TEST_EMAIL,
  })

  await expect(page.getByTestId('audio-upload')).toBeVisible({ timeout: 15000 })
  await page.context().storageState({ path: authFile })
})
