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
    if (response.status() >= 500) errors.push(`${response.status()} ${response.url()}`)
  })
  return errors
}

function isExpectedBrowserNoise(message: string) {
  return message.includes('Failed to load resource: the server responded with a status of 401 (Unauthorized)')
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
