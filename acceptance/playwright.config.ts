import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.ACCEPTANCE_BASE_URL ?? 'http://localhost:15173'
const coldStartProjects = process.env.QA_COLD_START === '1'
// The stability soak (#631) is slow and opt-in: QA_SOAK=1 (set by qa-journeys
// when suite names soak) is the only way it runs, so neither the required
// smoke nor the every-spec journeys check picks it up.
const soak = process.env.QA_SOAK === '1'
// ACCEPTANCE_EVIDENCE=1 (set by the on-demand qa-journeys workflow) keeps a
// trace + screenshot + video for every test, passing or not, so an independent
// QA agent can inspect the journeys without re-running them.
const evidence = process.env.ACCEPTANCE_EVIDENCE === '1'

export default defineConfig({
  testDir: '.',
  testIgnore: [
    ...(coldStartProjects ? [] : ['**/*.cold-start.spec.ts']),
    ...(soak ? [] : ['**/soak/**']),
  ],
  timeout: 30_000,
  fullyParallel: false,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'reports/html', open: 'never' }],
  ],
  outputDir: 'reports/artifacts',
  use: {
    baseURL,
    trace: evidence ? 'on' : 'retain-on-failure',
    screenshot: evidence ? 'on' : 'only-on-failure',
    video: evidence ? 'on' : 'off',
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
