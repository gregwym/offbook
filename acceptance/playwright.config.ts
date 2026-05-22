import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.ACCEPTANCE_BASE_URL ?? 'http://localhost:15173'
const coldStartProjects = process.env.QA_COLD_START === '1'

export default defineConfig({
  testDir: '.',
  testIgnore: coldStartProjects ? [] : ['**/*.cold-start.spec.ts'],
  timeout: 30_000,
  fullyParallel: false,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'reports/html', open: 'never' }],
  ],
  outputDir: 'reports/artifacts',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
  projects: coldStartProjects ? [
    {
      name: 'desktop-chromium',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1280, height: 900 },
      },
    },
  ] : [
    {
      name: 'desktop-chromium',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1280, height: 900 },
      },
    },
    {
      name: 'mobile-chromium',
      use: {
        browserName: 'chromium',
        isMobile: true,
        hasTouch: true,
        userAgent: devices['iPhone 14'].userAgent,
        viewport: { width: 393, height: 852 },
      },
    },
  ],
})
