import { defineConfig, devices } from '@playwright/test'

// `npm run screenshots` sets GENKAN_SHOTS=1 and runs only the screenshot project;
// the regular suite never writes screenshots.
const shots = !!process.env.GENKAN_SHOTS

export default defineConfig({
  testDir: './specs',
  globalSetup: './global-setup.ts',
  globalTeardown: './global-teardown.ts',
  // All specs share one container and one admin account, so run strictly serially.
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  timeout: 30_000,
  expect: { timeout: 7_000 },
  reporter: [['list']],
  outputDir: './test-results',
  use: {
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    actionTimeout: 8_000,
    locale: 'en-US',
  },
  projects: shots
    ? [{ name: 'screenshots', testMatch: /screenshots\.spec\.ts/, timeout: 180_000, use: { ...devices['Desktop Chrome'] } }]
    : [{ name: 'chromium', testIgnore: /screenshots\.spec\.ts/, use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } }],
})
