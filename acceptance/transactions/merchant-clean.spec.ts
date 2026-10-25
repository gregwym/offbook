import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Cleaned merchant names (#588). A statement CSV lists one café under two
// store numbers. After import both rows lead with the same readable name
// (the bank string stays visible), and Insights → Top merchants shows the
// café once with both amounts summed. Throwaway user; runs under both the
// desktop and the narrow mobile project.

test.describe('cleaned merchant names', () => {
  test('one café under two store numbers is one merchant', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `merchant-clean-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await apiPost(cookie, '/accounts', {
      name: 'Clean Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    const csv = [
      'Date,Description,Amount',
      `${day(1)},SQ *BLUE BTL 0412,-6.50`,
      `${day(2)},SQ *BLUE BTL 0415,-7.25`,
      `${day(3)},TST* JOE'S PIZZA #22,-18.40`,
    ].join('\n')

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')
    await page.getByRole('button', { name: 'Import transactions' }).first().click()
    await page.locator('select').first().selectOption({ label: 'Clean Checking' })
    await page.locator('input[type=file]').setInputFiles({ name: 'cafe.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
    await page.getByRole('button', { name: 'Preview' }).click()
    await page.getByRole('button', { name: 'Import 3 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()

    // The list leads with the cleaned name; the raw bank string is still on
    // screen (Bank description column on desktop, meta line on a phone).
    await page.goto('/transactions')
    for (const raw of ['SQ *BLUE BTL 0412', 'SQ *BLUE BTL 0415']) {
      const row = page.locator('tbody tr').filter({ hasText: raw })
      await expect(row.locator('td:nth-child(3) span.font-medium').first()).toHaveText('Blue Btl')
      await expect(row.getByText(raw, { exact: true }).locator('visible=true').first()).toBeVisible()
    }
    await expect(page.locator('tbody tr').filter({ hasText: "TST* JOE'S PIZZA #22" })
      .locator('td:nth-child(3) span.font-medium').first()).toHaveText("Joe's Pizza")
    await testInfo.attach(`${testInfo.project.name}-cleaned-list`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await expectNoOverflow(page)

    // Top merchants: one café row, both visits, summed exactly.
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    const merchants = band.locator('div', { has: page.getByRole('heading', { name: 'Top merchants' }) }).last()
    const cafe = merchants.getByRole('listitem').filter({ hasText: 'Blue Btl' })
    await expect(cafe).toHaveCount(1)
    await expect(cafe).toContainText('×2')
    await expect(cafe).toContainText('13.75')
    await expect(merchants.getByRole('listitem').filter({ hasText: "Joe's Pizza" })).toContainText('18.40')
    await expect(merchants.getByRole('listitem')).toHaveCount(2)
    await merchants.scrollIntoViewIfNeeded()
    await testInfo.attach(`${testInfo.project.name}-top-merchants`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await expectNoOverflow(page)

    const top = (await apiGet(cookie, '/dashboard/top-merchants?limit=20')).data as { merchant: string, amount: string, count: number }[]
    expect(top.find((m) => m.merchant === 'Blue Btl')).toMatchObject({ amount: '13.75', count: 2 })
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function apiGet(cookie: string, path: string) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, { headers: { cookie } })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
  return res.json()
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
