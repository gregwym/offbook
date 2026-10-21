import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for the Needs-review inbox (#424): imported rows start unreviewed,
// can be reviewed in bulk, undone, and the inbox reaches an "all caught up"
// state. Uses a throwaway user so it never touches shared persona data.
// Runs under both the desktop and the narrow mobile project.

const rows = ['Review Coffee', 'Review Grocer', 'Review Fuel']

test.describe('transaction review inbox', () => {
  test('bulk review, undo, inbox zero', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `review-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await api(cookie, '/accounts', {
      name: 'Review Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    for (const [i, d] of rows.entries()) {
      await api(cookie, '/transactions', {
        account_id: acct.data.id, amount: `-${12 + i}.34`, description: d,
        transaction_date: '2026-09-1' + (i + 1), source: 'csv',
      })
    }

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    await page.getByRole('button', { name: /needs review/i }).first().click()
    for (const d of rows) await expect(page.getByText(d)).toBeVisible()
    await expectNoOverflow(page)

    // Bulk review two rows.
    await page.getByLabel('Select Review Coffee').check()
    await page.getByLabel('Select Review Grocer').check()
    await page.getByRole('region', { name: 'Bulk actions' }).getByRole('button', { name: /mark reviewed/i }).click()
    await expect(page.getByRole('status')).toContainText('2 transactions marked reviewed')
    await expect(page.getByText('Review Coffee')).toHaveCount(0)
    await expect(page.getByText('Review Fuel')).toBeVisible()
    await expectNoOverflow(page)

    // Undo brings them back.
    await page.getByRole('status').getByRole('button', { name: 'Undo' }).click()
    await expect(page.getByText('Review Coffee')).toBeVisible()

    // Persistence: reload keeps the reviewed state server-side.
    await page.getByLabel('Select Review Coffee').check()
    await page.getByLabel('Select Review Grocer').check()
    await page.getByLabel('Select Review Fuel').check()
    await page.getByRole('region', { name: 'Bulk actions' }).getByRole('button', { name: /mark reviewed/i }).click()
    await expect(page.getByText(/all caught up/i)).toBeVisible()
    await page.reload()
    // The filter is URL-persisted (?review=1): after reload the chip is still
    // active, so assert that instead of clicking (which would toggle it off).
    await expect(page.getByRole('button', { name: /needs review/i }).first()).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByText(/all caught up/i)).toBeVisible()
    await expectNoOverflow(page)
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function api(cookie: string, path: string, body: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method: 'POST', headers: { 'content-type': 'application/json', cookie }, body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
  return res.json()
}

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill(email)
  await page.getByLabel(/password/i).fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/h\/insights|\/accounts/)
}

async function expectNoOverflow(page: Page) {
  const o = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
  expect(o, 'no horizontal overflow').toBe(false)
}
