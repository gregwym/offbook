import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Failed-import recovery journey (#639): a statement with one unreadable row
// past the first screen of the preview shows that row and says it is skipped;
// the good rows import, the fixed file then brings in only the repaired row,
// and an empty file explains itself and recovers when a valid file is chosen.
// Throwaway user; runs under both the desktop and the narrow mobile project.

const header = 'Date,Description,Amount'
const good = Array.from({ length: 19 }, (_, i) => `2026-09-${String(i + 1).padStart(2, '0')},Recov Shop ${i + 1},-${i + 1}.25`)
const badLine = '13/45/2026,Recov Broken Date,-42.00'
const fixedLine = '2026-09-20,Recov Broken Date,-42.00'
// Header is line 1, so the bad row sits on line 18 — well past the 12-row sample.
const withBad = [header, ...good.slice(0, 16), badLine, ...good.slice(16)].join('\n')
const fixed = [header, ...good.slice(0, 16), fixedLine, ...good.slice(16)].join('\n')

test.describe('import recovery', () => {
  test('unreadable row is shown and skipped; fixed file adds only it; empty file recovers', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `recov-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await apiPost(cookie, '/accounts', {
      name: 'Recov Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')

    // Empty file: plain-language error, and a valid file recovers.
    await openImport(page)
    await chooseStatement(page, 'empty.csv', '')
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText(/This file has no transactions in it\./)).toBeVisible()
    await expect(page.getByText('file is empty', { exact: true })).toHaveCount(0)
    await testInfo.attach('import-empty-file', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    await chooseStatement(page, 'september.csv', withBad)
    await expect(page.getByText(/This file has no transactions in it\./)).toHaveCount(0)
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('19 new', { exact: true })).toBeVisible()
    await expect(page.getByText('1 skipped', { exact: true })).toBeVisible()
    await expect(page.getByText(/1 row can’t be read and will be skipped/)).toBeVisible()
    const badRow = page.locator('tbody tr').filter({ hasText: 'unparseable date "13/45/2026"' })
    await expect(badRow).toBeVisible()
    await expect(badRow.getByText('line 18').first()).toBeAttached()
    await expectNoOverflow(page)
    await testInfo.attach('import-row-skipped', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await page.getByRole('button', { name: 'Import 19 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()

    // Fixed file: only the repaired row is new.
    await openImport(page)
    await chooseStatement(page, 'september-fixed.csv', fixed)
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('1 new', { exact: true })).toBeVisible()
    await expect(page.getByText('19 duplicate', { exact: true })).toBeVisible()
    await expect(page.getByText(/can’t be read/)).toHaveCount(0)
    await testInfo.attach('import-fixed-file', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await page.getByRole('button', { name: 'Import 1 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()

    // Ledger: 20 rows, each exactly once.
    const txns = await apiGet(cookie, '/transactions?limit=200')
    const descs = (txns.data as { description: string }[]).map((t) => t.description).filter((d) => d.startsWith('Recov '))
    expect(descs).toHaveLength(20)
    expect(new Set(descs).size).toBe(20)
    await page.goto('/transactions')
    await expect(page.getByText('Recov Broken Date').first()).toBeVisible()
  })
})

async function openImport(page: Page) {
  await page.getByRole('button', { name: 'Import transactions' }).first().click()
  await page.locator('select').first().selectOption({ label: 'Recov Checking' })
}

async function chooseStatement(page: Page, name: string, body: string) {
  await page.locator('input[type=file]').setInputFiles({ name, mimeType: 'text/csv', buffer: Buffer.from(body) })
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

async function apiGet(cookie: string, path: string) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, { headers: { cookie } })
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
