import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser, withQAClient } from '../fixtures/state.mjs'

// Journey for the household Insights asset-class drill-in (#374 / #492): the
// "By asset class" disclosure under the household net-worth band must show only
// shared-account value. A private account in the same household must not leak
// into the totals. Throwaway household; runs under desktop and narrow mobile.

const isoMonth = (offset: number, day: number) => {
  const d = new Date()
  const y = d.getUTCFullYear()
  const m = d.getUTCMonth() + offset
  const dt = new Date(Date.UTC(y, m, day))
  return dt.toISOString().slice(0, 10)
}

test.describe('household insights asset-class drill-in', () => {
  test('shows shared cash by class and excludes private accounts', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const suite = `hhdrill-${testInfo.project.name}`
    const householdID = await withQAClient(async (client) => {
      const r = await client.query(
        "INSERT INTO households (name, grace_period_days, created_at, updated_at) VALUES ('Drill-in Household', 30, NOW(), NOW()) RETURNING id",
      )
      return Number(r.rows[0].id)
    })
    const user = await createThrowawayUser({ suite, householdID, role: 'owner' })
    const cookie = await apiSignin(user.email, user.password)

    const shared = await createAccount(cookie, 'Shared Checking')
    const priv = await createAccount(cookie, 'Private Stash')
    await importCSV(cookie, shared, [
      'Date,Description,Amount',
      `${isoMonth(-2, 10)},Drill Payroll,1000.00`,
      `${isoMonth(-1, 12)},Drill Grocer,-200.00`,
    ])
    await importCSV(cookie, priv, ['Date,Description,Amount', `${isoMonth(-1, 5)},Drill Secret Deposit,777.00`])
    await apiPut(cookie, `/accounts/${shared}/shares/${householdID}`, { visibility: 'balance_and_txns' })

    await signIn(page, user.email, user.password)
    await page.goto('/h/insights')
    await expect(page.getByText('800.00 USD').first()).toBeVisible()

    await page.getByText('By asset class').first().click()
    const table = page.getByRole('table', { name: /net worth by asset class/i })
    await expect(table).toBeVisible()
    const cash = table.getByRole('row', { name: /cash/i })
    await expect(cash).toContainText('800.00')
    // The private account's 777.00 must not appear in any figure.
    await expect(page.locator('body')).not.toContainText('1,577.00')
    await expect(page.locator('body')).not.toContainText('777.00')
    await expectNoOverflow(page)
  })
})

async function createAccount(cookie: string, name: string): Promise<number> {
  const res = await fetch(`${acceptanceAPIURL()}/accounts`, {
    method: 'POST',
    headers: { cookie, 'content-type': 'application/json' },
    body: JSON.stringify({ name, institution_slug: 'manual', account_type: 'checking', currency: 'USD' }),
  })
  if (!res.ok) throw new Error(`account ${res.status} ${await res.text()}`)
  return (await res.json()).data.id
}

async function importCSV(cookie: string, accountID: number, lines: string[]) {
  const form = new FormData()
  form.append('file', new Blob([lines.join('\n')], { type: 'text/csv' }), 'drill.csv')
  form.append('commit', 'true')
  const res = await fetch(`${acceptanceAPIURL()}/accounts/${accountID}/transactions/import`, {
    method: 'POST', headers: { cookie }, body: form,
  })
  if (!res.ok) throw new Error(`import ${res.status} ${await res.text()}`)
}

async function apiPut(cookie: string, path: string, body: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method: 'PUT', headers: { cookie, 'content-type': 'application/json' }, body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
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
