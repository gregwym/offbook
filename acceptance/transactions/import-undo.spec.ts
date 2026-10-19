import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for Undo Import (IMP-01 / #477): a committed CSV import appears in the
// account's import history, can be undone after an explicit confirmation, its
// rows disappear from Transactions, and the same statement can be imported again.
// Throwaway user; runs under both the desktop and the narrow mobile project.

const csv = [
  'Date,Description,Amount',
  '2026-09-01,Undo Coffee,-4.50',
  '2026-09-02,Undo Grocer,-52.10',
  '2026-09-03,Undo Payroll,1200.00',
].join('\n')

test.describe('undo statement import', () => {
  test('undo removes imported rows and allows re-import', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `undo-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await apiPost(cookie, '/accounts', {
      name: 'Undo Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const accountID = acct.data.id as number
    await importCSV(cookie, accountID)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    for (const d of ['Undo Coffee', 'Undo Grocer', 'Undo Payroll']) await expect(page.getByText(d)).toBeVisible()

    await page.goto('/accounts')
    await page.getByRole('button', { name: 'Import transactions' }).first().click()
    const history = page.getByRole('region', { name: 'Import history' })
    await expect(history).toContainText('3 imported')
    await expectNoOverflow(page)

    // Keep leaves the import intact.
    await history.getByRole('button', { name: 'Undo import' }).click()
    await expect(history).toContainText('Remove the 3 transactions')
    await history.getByRole('button', { name: 'Keep' }).click()
    await expect(history.getByRole('button', { name: 'Undo import' })).toBeVisible()

    // Confirmed undo.
    await history.getByRole('button', { name: 'Undo import' }).click()
    await history.getByRole('button', { name: 'Yes, undo import' }).click()
    await expect(history).toContainText(/Undone .* 3 transactions removed/)
    await expect(history.getByRole('button', { name: 'Undo import' })).toHaveCount(0)
    await expectNoOverflow(page)

    await page.goto('/transactions')
    await expect(page.getByText('Undo Coffee')).toHaveCount(0)

    // The same statement can be imported again (no duplicate carry-over).
    const again = await importCSV(cookie, accountID)
    expect(again.data.inserted_count).toBe(3)
    expect(again.data.duplicate_count).toBe(0)
    await page.reload()
    await expect(page.getByText('Undo Coffee')).toBeVisible()
  })
})

async function importCSV(cookie: string, accountID: number) {
  const form = new FormData()
  form.append('file', new Blob([csv], { type: 'text/csv' }), 'undo-statement.csv')
  form.append('commit', 'true')
  const res = await fetch(`${acceptanceAPIURL()}/accounts/${accountID}/transactions/import`, {
    method: 'POST', headers: { cookie }, body: form,
  })
  if (!res.ok) throw new Error(`import ${res.status} ${await res.text()}`)
  return res.json()
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function apiPost(cookie: string, path: string, body: unknown) {
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
