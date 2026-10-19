import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for duplicate statement upload (Daily Driver charter): re-uploading the
// same CSV previews as all-duplicate with nothing importable, and an overlapping
// statement imports only its new rows, so Transactions never double-counts.
// Throwaway user; runs under both the desktop and the narrow mobile project.

const march = ['Date,Description,Amount', '2026-09-01,Dup Coffee,-4.50', '2026-09-02,Dup Grocer,-52.10'].join('\n')
const overlap = [march, '2026-09-03,Dup Payroll,1200.00'].join('\n')

test.describe('duplicate statement upload', () => {
  test('re-upload is all duplicate; overlap imports only new rows', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `dup-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await apiPost(cookie, '/accounts', {
      name: 'Dup Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')

    // First upload: everything is new.
    await openImport(page)
    await chooseStatement(page, 'march.csv', march)
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('2 new', { exact: true })).toBeVisible()
    await expect(page.getByText('0 duplicate', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Import 2 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()

    // Same file again: all duplicates, nothing to import.
    await openImport(page)
    await chooseStatement(page, 'march.csv', march)
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('0 new', { exact: true })).toBeVisible()
    await expect(page.getByText('2 duplicate', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Import', exact: true })).toBeDisabled()
    await expectNoOverflow(page)

    // Overlapping statement: only the new row lands.
    await chooseStatement(page, 'overlap.csv', overlap)
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('1 new', { exact: true })).toBeVisible()
    await expect(page.getByText('2 duplicate', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Import 1 new' }).click()

    await page.goto('/transactions')
    for (const d of ['Dup Coffee', 'Dup Grocer', 'Dup Payroll']) await expect(page.getByText(d)).toHaveCount(1)
  })
})

async function openImport(page: Page) {
  await page.getByRole('button', { name: 'Import transactions' }).first().click()
  await page.locator('select').first().selectOption({ label: 'Dup Checking' })
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
