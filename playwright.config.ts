import { defineConfig, devices } from '@playwright/test'
import dotenv from 'dotenv'
import path from 'path'

dotenv.config({ path: path.resolve(__dirname, '.env') })

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: 'http://localhost:5173',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'setup',
      testMatch: /global\.setup\.ts/,
    },
    {
      name: 'signed-out',
      testMatch: /signed-out\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
      dependencies: ['setup'],
    },
    {
      name: 'authenticated',
      testMatch: /(drive-setup|students|upload-jobs|reports)\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'playwright/.clerk/user.json',
      },
      dependencies: ['setup'],
    },
    {
      name: 'api',
      testMatch: /api-health\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
      dependencies: ['setup'],
    },
  ],
  webServer: [
    {
      command: 'npm run dev:frontend',
      url: 'http://localhost:5173',
      reuseExistingServer: !process.env.CI,
    },
    {
      command: 'npm run dev:backend',
      url: 'http://localhost:8080/health',
      reuseExistingServer: !process.env.CI,
    },
  ],
})
