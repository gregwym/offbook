import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for a stale holding price (#530, Daily Driver gate 4): a brokerage
// buy recorded a month ago has only that trade's price, which is now older
// than the 7-day freshness window. Accounts must flag the balance partial
// (never value the holding at $0 silently), name the holding that needs a
// price, and open Set price pre-filled with it; saving a price makes the
// balance complete and correct. Throwaway user and a run-unique ticker, since
// prices are instance-wide reference data. Runs under desktop and mobile.

test.describe('stale holding price', () => {
  test('partial balance names the holding and Set price completes it', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `price-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const symbol = `QP${Date.now().toString(36).toUpperCase()}${testInfo.project.name.startsWith('mobile') ? 'M' : 'D'}`
    const acct = await api(cookie, '/accounts', {
      name: 'Price Brokerage', institution_slug: 'manual', account_type: 'investment', currency: 'USD', balance: '1000.00',
    })
    const asset = await api(cookie, '/assets/ensure', { symbol, kind: 'equity' })
    const monthAgo = new Date(Date.now() - 30 * 24 * 3600 * 1000).toISOString().slice(0, 10)
    await api(cookie, `/accounts/${acct.data.id}/trades`, {
      kind: 'buy', asset_id: asset.data.id, quantity: '2', price: '40.00', trade_date: monthAgo,
    })

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')
    const row = page.getByRole('row').filter({ hasText: 'Price Brokerage' })
    // Cash 920 counts; the holding is left out and flagged, not valued at 0.
    await expect(row).toContainText('920.00')
    await expect(row.getByText('partial', { exact: true })).toBeVisible()
    await expect(row).toContainText(`Needs a price: ${symbol}`)
    const setPrice = row.getByRole('button', { name: `Set price for ${symbol}` })
    await expectInViewport(page, setPrice)
    await testInfo.attach('accounts-needs-price', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    await setPrice.click()
    const dialog = page.getByRole('dialog', { name: `Set price for ${symbol}` })
    await expect(dialog).toContainText('no price from the last 7 days')
    await expect(dialog.getByPlaceholder('e.g. MYFUND')).toHaveValue(symbol)
    const priceInput = dialog.getByPlaceholder('100.00')
    await expect(priceInput).toBeFocused()
    await expectNoOverflow(page)
    await priceInput.fill('50.25')
    await testInfo.attach('set-price-prefilled', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dialog.getByRole('button', { name: 'Set price' }).click()
    await expect(dialog).toHaveCount(0)

    // 920 cash + 2 × 50.25 = 1,020.50, complete.
    await expect(row).toContainText('1,020.50')
    await expect(row.getByText('partial', { exact: true })).toHaveCount(0)
    await expect(row.getByText(/Needs a price/)).toHaveCount(0)
    await page.reload()
    await expect(row).toContainText('1,020.50')
    await expect(row.getByText(/Needs a price/)).toHaveCount(0)
    await testInfo.attach('accounts-priced', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    const accounts = (await get(cookie, '/accounts')).data as { id: number; balance_complete: boolean; unpriced_assets: unknown[] }[]
    const a = accounts.find((x) => x.id === acct.data.id)!
    expect(a.balance_complete).toBe(true)
    expect(a.unpriced_assets).toEqual([])
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
