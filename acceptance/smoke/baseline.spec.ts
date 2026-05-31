import { expect, test, type Page } from '@playwright/test'
import { personaPassword } from '../fixtures/env.mjs'

const publicRoutes = ['/setup/admin', '/signin', '/signup']
const personalRoutes = [
  '/insights',
  '/accounts',
  '/accounts/add',
  '/transactions',
  '/budgets',
  '/savings-goals',
  '/investments',
  '/settings',
]
const householdRoutes = ['/h/insights', '/h/budgets', '/h/goals', '/h/members', '/h/ai', '/h/settings']

test.describe('baseline route smoke', () => {
  test('response classifier accepts known unauthenticated probes', () => {
    // Bootstrap probes that legitimately 401 before sign-in.
    expect(isAPIFailure(401, 'http://localhost:18000/api/v1/me')).toBe(false)
    expect(isAPIFailure(401, 'http://localhost:18000/api/v1/setup/status')).toBe(false)
    // Routes that should not return auth failures once we've signed in.
    expect(isAPIFailure(401, 'http://localhost:18000/api/v1/dashboard/summary')).toBe(true)
    expect(isAPIFailure(403, 'http://localhost:18000/api/v1/h/dashboard')).toBe(true)
  })

  test('response classifier flags 4xx on API routes', () => {
    // The exact regressions #266 (Insights 404) and #268 (Investments 404)
    // would have been caught here if this test ran in CI back then.
    expect(isAPIFailure(404, 'http://localhost:18000/api/v1/investments/portfolio')).toBe(true)
    expect(isAPIFailure(404, 'http://localhost:18000/api/v1/investments')).toBe(true)
    expect(isAPIFailure(400, 'http://localhost:18000/api/v1/budgets')).toBe(true)
    // 5xx on any URL is always a failure.
    expect(isAPIFailure(500, 'http://localhost:18000/api/v1/anything')).toBe(true)
    expect(isAPIFailure(503, 'http://localhost:5173/anything')).toBe(true)
  })

  test('response classifier ignores non-API 4xx', () => {
    // Failed asset / favicon / third-party fetches are someone else's
    // problem. The smoke is about API contract failures.
    expect(isAPIFailure(404, 'http://localhost:18000/favicon.ico')).toBe(false)
    expect(isAPIFailure(404, 'http://localhost:5173/some-asset.png')).toBe(false)
    expect(isAPIFailure(200, 'http://localhost:18000/api/v1/anything')).toBe(false)
  })

  for (const route of publicRoutes) {
    test(`public route ${route}`, async ({ page }) => {
      const errors = collectRuntimeErrors(page)
      await page.goto(route)
      await expect(page.locator('body')).not.toBeEmpty()
      await expectNoHorizontalOverflow(page)
      expect(errors, 'console errors, uncaught exceptions, or API 4xx/5xx').toEqual([])
    })
  }

  for (const route of [...personalRoutes, ...householdRoutes]) {
    test(`authenticated route ${route}`, async ({ page }) => {
      const errors = collectRuntimeErrors(page)
      await login(page)
      await page.goto(route)
      await expect(page.locator('body')).not.toBeEmpty()
      await expectNoHorizontalOverflow(page)
      expect(errors, 'console errors, uncaught exceptions, or API 4xx/5xx').toEqual([])
    })
  }
})

function collectRuntimeErrors(page: Page) {
  const errors: string[] = []
  page.on('console', (msg) => {
    if (msg.type() === 'error' && !isExpectedBrowserNoise(msg.text())) errors.push(msg.text())
  })
  page.on('pageerror', (err) => errors.push(err.message))
  page.on('response', (response) => {
    if (isAPIFailure(response.status(), response.url())) {
      errors.push(`${response.status()} ${response.url()}`)
    }
  })
  return errors
}

function isExpectedBrowserNoise(message: string) {
  // Chromium reports failed 401 fetches as console errors without a reliable URL.
  // Response listeners do have the URL, so auth regressions are checked there.
  return message.includes('Failed to load resource: the server responded with a status of 401 (Unauthorized)')
}

// isAPIFailure decides whether a single HTTP response should fail the smoke.
//
// Policy (epic #270, L1 / #271):
//   - 5xx anywhere → always fail. The server crashed.
//   - 4xx on /api/v1/* → fail UNLESS it's a known unauthenticated probe.
//     This is the layer that catches contract drift like #266 / #268 — a
//     frontend call to a route the backend doesn't expose.
//   - 4xx on non-/api URLs → ignore. Failed favicons, missing optional
//     assets, and third-party fetches aren't this suite's contract.
//   - 1xx/2xx/3xx → always fine.
//
// "Known unauthenticated probes" cover the auth-bootstrap hits that fire
// before sign-in (and legitimately 401 if no session cookie exists). Add
// new entries here when a new bootstrap call is added to the frontend.
function isAPIFailure(status: number, url: string): boolean {
  if (status >= 500) return true
  if (status < 400) return false
  // 4xx territory.
  if (!url.includes('/api/v1/')) return false
  if ((status === 401 || status === 403) && isExpectedUnauthenticatedProbe(url)) return false
  return true
}

function isExpectedUnauthenticatedProbe(url: string): boolean {
  return url.includes('/api/v1/me') || url.includes('/api/v1/setup/status')
}

async function login(page: Page) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill('qa-admin@offbook.local')
  await page.getByLabel(/password/i).fill(personaPassword('qa-admin@offbook.local'))
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/h\/insights/)
}

async function expectNoHorizontalOverflow(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
  expect(overflow, 'document must not horizontally overflow').toBe(false)
}
