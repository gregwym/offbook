import { expect, test, type Page } from '@playwright/test'
import { personaPassword } from '../fixtures/env.mjs'

const publicRoutes = ['/setup/admin', '/signin', '/signup']
const personalRoutes = [
  '/dashboard',
  '/accounts',
  '/connect',
  '/transactions',
  '/rules',
  '/budgets',
  '/savings-goals',
  '/investments',
  '/import',
  '/ai',
  '/settings',
]
const householdRoutes = ['/h/dashboard', '/h/budgets', '/h/goals', '/h/members', '/h/ai', '/h/settings']

test.describe('baseline route smoke', () => {
  test('unexpected auth failures are reported', () => {
    expect(isUnexpectedAuthFailure(401, 'http://localhost:18000/api/v1/dashboard/summary')).toBe(true)
    expect(isUnexpectedAuthFailure(403, 'http://localhost:18000/api/v1/h/dashboard/summary')).toBe(true)
    expect(isUnexpectedAuthFailure(401, 'http://localhost:18000/api/v1/me')).toBe(false)
  })

  for (const route of publicRoutes) {
    test(`public route ${route}`, async ({ page }) => {
      const errors = collectRuntimeErrors(page)
      await page.goto(route)
      await expect(page.locator('body')).not.toBeEmpty()
      await expectNoHorizontalOverflow(page)
      expect(errors, 'console errors, uncaught exceptions, or 5xx responses').toEqual([])
    })
  }

  for (const route of [...personalRoutes, ...householdRoutes]) {
    test(`authenticated route ${route}`, async ({ page }) => {
      const errors = collectRuntimeErrors(page)
      await login(page)
      await page.goto(route)
      await expect(page.locator('body')).not.toBeEmpty()
      await expectNoHorizontalOverflow(page)
      expect(errors, 'console errors, uncaught exceptions, or 5xx responses').toEqual([])
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
    if (response.status() >= 500 || isUnexpectedAuthFailure(response.status(), response.url())) {
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

function isUnexpectedAuthFailure(status: number, url: string) {
  if (status !== 401 && status !== 403) return false

  const expectedUnauthenticatedProbe = url.includes('/api/v1/me')
  return !expectedUnauthenticatedProbe
}

async function login(page: Page) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill('qa-admin@offbook.local')
  await page.getByLabel(/password/i).fill(personaPassword('qa-admin@offbook.local'))
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/dashboard|\/h\/dashboard/)
}

async function expectNoHorizontalOverflow(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
  expect(overflow, 'document must not horizontally overflow').toBe(false)
}
