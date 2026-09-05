import { defineConfig, devices } from '@playwright/test'
import dotenv from 'dotenv'
import path from 'path'

dotenv.config({ path: path.resolve(__dirname, '.env') })

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  // One retry locally as well. Roughly one run in five fails a single spec on a
  // first page load that never renders: the whole app sits inside Clerk's
  // <Show>, so a slow load always looks like a random spec not finding its
  // container. It clusters in a fresh worktree's first runs, and the only
  // condition it reproduced under was a cold vite dep cache (1 of 5 runs with
  // node_modules/.vite cleared; 12 warm-cache runs never failed, including cold
  // servers, a full make test, and two suites at once). Cause not pinned
  // further — the trace kept on retry is the passing attempt's.
  //
  // Playwright still reports a passing retry as flaky, so an intermittent bug
  // stays visible rather than being swallowed.
  retries: process.env.CI ? 2 : 1,
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
      // Catch-all, minus the specs that run in the projects above. Enumerating
      // specs by name here let feedback.spec.ts sit in e2e/ for months without
      // ever running; a new spec now joins this project by default, and a spec
      // filed in the wrong project fails loudly instead of silently not running.
      testMatch: /\.spec\.ts$/,
      testIgnore: /(signed-out|api-health)\.spec\.ts/,
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
      command: 'pnpm run dev:frontend',
      url: 'http://localhost:5173',
      reuseExistingServer: !process.env.CI,
    },
    {
      command: 'pnpm run dev:backend',
      url: 'http://localhost:8080/health',
      timeout: 120_000,
      reuseExistingServer: !process.env.CI,
    },
  ],
})
