import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1/3 journey (TX-02 + splits): a user splits one receipt
// across two categories and hides a one-off transaction. Spending on Insights
// must reflect both, and un-hiding must restore it. Throwaway user; runs under
// both desktop and mobile projects.

test.describe('split and hide journey', () => {
  test('split allocates across categories; hide removes from spending and is reversible', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `splithide-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const cat = (slug: string) => cats.data.find((x: { slug: string }) => x.slug === slug) as { id: number; name: string }
    const groceries = cat('groceries')
    const gas = cat('gas')
    const acct = (await api(cookie, '/accounts', {
      name: 'Split Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const today = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (description: string, amount: string, category_id?: number) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv', category_id })
    await tx('QASPLIT Superstore', '-100.00', groceries.id)
    await tx('QASPLIT One-off gadget', '-40.00', gas.id)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    await expect(page.getByText('QASPLIT Superstore').first()).toBeVisible()

    // Split $100 into $70 groceries + $30 gas.
    await rowAction(page, 'QASPLIT Superstore', 'Split QASPLIT Superstore')
    const dlg = page.getByRole('dialog', { name: 'Split QASPLIT Superstore' })
    await expect(dlg).toBeVisible()
    // Over-allocating shows a formatted amount (#506).
    await dlg.getByLabel('Amount for part 2').fill('130.00')
    await expect(dlg.getByRole('status')).toHaveText('Over-allocated by 130.00 USD.')
    await dlg.getByLabel('Amount for part 2').fill('')
    await expect(dlg.getByRole('status')).toContainText('every part needs a non-zero amount')
    await page.keyboard.press('Escape')
    await expect(dlg).toBeHidden()

    // A fresh split auto-fills the last part: one number is enough (#506).
    await rowAction(page, 'QASPLIT Superstore', 'Split QASPLIT Superstore')
    await dlg.getByLabel('Amount for part 1').fill('70.00')
    await expect(dlg.getByLabel('Amount for part 2')).toHaveValue('30.00')
    await dlg.getByLabel('Category for part 2').selectOption({ label: gas.name })
    await expect(dlg.getByRole('status')).toHaveText('Fully allocated.')
    await expect(dlg.getByRole('button', { name: 'Save split' })).toBeEnabled()
    await testInfo.attach(`${testInfo.project.name}-split-autofill`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dlg.getByRole('button', { name: 'Save split' }).click()
    await expect(dlg).toBeHidden()
    await expect(page.getByRole('button', { name: 'Edit split for QASPLIT Superstore' })).toBeVisible()

    let spend = await byCategory(cookie, now)
    expect(spend.groceries).toBe(70)
    expect(spend.gas).toBe(70) // 30 from split + 40 gadget

    // Hide the gadget: gone from spending, still in the ledger, reversible.
    await rowAction(page, 'QASPLIT One-off gadget', 'Hide QASPLIT One-off gadget')
    await expect(page.locator('span:visible', { hasText: /^hidden$/ }).first()).toBeVisible()
    await expect(page.getByText('QASPLIT One-off gadget').locator('visible=true').first()).toBeVisible()
    await expect.poll(async () => (await byCategory(cookie, now)).gas).toBe(30)

    await page.reload()
    await expect(page.locator('span:visible', { hasText: /^hidden$/ }).first()).toBeVisible()

    await rowAction(page, 'QASPLIT One-off gadget', 'Unhide QASPLIT One-off gadget')
    await expect.poll(async () => (await byCategory(cookie, now)).gas).toBe(70)
    await expect(page.locator('span:visible', { hasText: /^hidden$/ })).toHaveCount(0)
    await expectNoOverflow(page)
  })
})

async function byCategory(cookie: string, _now: Date) {
  const res = await get(cookie, '/dashboard/spend-by-category?period=current_month')
  const out: Record<string, number> = {}
  for (const c of res.data ?? []) out[c.name.toLowerCase()] = Math.abs(Number(c.amount))
  return out
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function get(cookie: string, path: string) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, { headers: { cookie } })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
  return res.json()
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

// A row's secondary actions sit behind "More actions" at every width
// (#554 phone, #563 desktop).
async function rowAction(page: Page, label: string, name: string) {
  await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
  await page.getByRole('menu').getByRole('menuitem', { name, exact: true }).click()
}
