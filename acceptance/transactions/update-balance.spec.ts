import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for manual "Update balance" (#504, Daily Driver gate 4): a manual
// account's balance is set to what the bank reports, the first update anchors
// an opening balance, a later one writes an adjustment that flags the account
// and is explained + acknowledged on the Reconciliation page, and neither
// counts as spending. Throwaway user; runs under desktop and mobile projects.

test.describe('update manual account balance', () => {
  test('update balance, reconcile, acknowledge', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `balance-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await api(cookie, '/accounts', {
      name: 'Balance Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const accountID = acct.data.id as number
    const now = new Date()
    const day1 = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (amount: string, description: string) =>
      api(cookie, '/transactions', { account_id: accountID, amount, description, transaction_date: day1, source: 'csv' })
    await tx('1200.00', 'QA Payroll')
    await tx('-100.00', 'QA Grocer')

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')
    const row = page.getByRole('row').filter({ hasText: 'Balance Checking' })
    await expect(row).toContainText('1,100.00')
    // #511: the balance, its action, sync state and row actions are on
    // screen at every width, not behind a sideways scroll of the table.
    await expectInViewport(page, row.getByText('1,100.00 USD', { exact: true }))
    await expectInViewport(page, row.getByRole('button', { name: 'Update balance for Balance Checking' }))
    await expectInViewport(page, row.getByRole('button', { name: /Import transactions/ }))
    await expectInViewport(page, row.getByRole('button', { name: /^Edit/ }))
    await expectInViewport(page, row.getByRole('button', { name: /^Delete/ }))
    await expect(page.getByText('Manage your accounts and PII.')).toHaveCount(0)
    await testInfo.attach('accounts-list', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    // First update: no anchor yet, so the difference becomes the opening balance.
    await row.getByRole('button', { name: 'Update balance for Balance Checking' }).click()
    const dialog = page.getByRole('dialog', { name: /Update balance/ })
    const input = dialog.getByLabel('Balance (USD)')
    await expect(input).toHaveValue('1100')
    await expectNoOverflow(page)
    // Validation keeps the dialog open.
    await input.fill('lots')
    await dialog.getByRole('button', { name: 'Save balance' }).click()
    await expect(dialog.getByRole('alert')).toContainText('Enter the balance as a number')
    await input.fill('2500.00')
    await dialog.getByRole('button', { name: 'Save balance' }).click()
    await expect(dialog.getByRole('status')).toContainText('2,500.00')
    await expect(dialog.getByRole('status')).toContainText('an opening balance of')
    await expect(dialog.getByRole('status')).toContainText('1,400.00')
    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(row).toContainText('2,500.00')
    // Observed now, not as of midnight UTC (which read "23h ago").
    await expect(row).toContainText(/Observed (just now|\dm ago) via manual/)
    await expect(row.getByText('needs attention')).toHaveCount(0)

    // Second update: a 150.00 shortfall is an adjustment (10% > threshold).
    await row.getByRole('button', { name: 'Update balance for Balance Checking' }).click()
    await input.fill('2350.00')
    await dialog.getByRole('button', { name: 'Save balance' }).click()
    await expect(dialog.getByRole('status')).toContainText('an adjustment of')
    await expect(dialog.getByRole('status')).toContainText('150.00')
    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(row).toContainText('2,350.00')
    await expect(row.getByRole('link', { name: /needs attention/ })).toBeVisible()

    // Adjustments and opening balances are not spending.
    const summary = await get(cookie, '/dashboard/summary')
    expect(Number(summary.data.spending)).toBe(100)
    expect(Number(summary.data.income)).toBe(1200)

    // The reconciliation view explains both and lets the user acknowledge.
    await row.getByRole('link', { name: /needs attention/ }).click()
    await expect(page).toHaveURL(new RegExp(`/accounts/${accountID}/reconciliation$`))
    await expect(page.getByRole('heading', { name: 'Reconciliation: Balance Checking' })).toBeVisible()
    const table = page.getByRole('table')
    await expect(table.getByRole('row').filter({ hasText: 'Opening balance' })).toContainText('manual')
    const adj = table.getByRole('row').filter({ hasText: 'Adjustment' })
    await expect(adj).toContainText('2,350.00')
    await expect(adj).toContainText('150.00')
    await adj.getByRole('button', { name: /Acknowledge/ }).click()
    await page.getByPlaceholder(/Optional note/).fill('Monthly fee per bank statement')
    await page.getByRole('button', { name: 'Acknowledge', exact: true }).click()
    await expect(adj).toContainText('Reviewed')
    await expectNoOverflow(page)

    await page.goto('/accounts')
    await expect(row).toContainText('2,350.00')
    await expect(row.getByText('needs attention')).toHaveCount(0)
    await page.reload()
    await expect(row).toContainText('2,350.00')
  })
})

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

async function expectInViewport(page: Page, loc: ReturnType<Page['getByRole']>) {
  await expect(loc).toBeVisible()
  const box = await loc.boundingBox()
  const vw = page.viewportSize()!.width
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'left edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'right edge on screen').toBeLessThanOrEqual(vw)
}
