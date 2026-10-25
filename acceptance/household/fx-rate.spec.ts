import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser, withQAClient } from '../fixtures/state.mjs'

// Household half of the missing-exchange-rate journey (#601; personal is
// #599). A household owner shares a USD account and a foreign-currency
// account with no FX rate on file, and keeps a second foreign account
// private. /h/insights must flag net worth partial, name only the shared
// currency ("Needs an exchange rate: HUF → USD"), never the private one, and
// Set rate must complete the household headline to the cent. Rates are
// instance-wide reference data, so each project uses its own currencies, none
// shared with transactions/fx-rate.spec.ts. Runs under desktop and mobile.

test.describe('household missing exchange rate', () => {
  test('partial household net worth names the shared currency and Set rate completes it', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const mobile = testInfo.project.name.startsWith('mobile')
    // HUF 100,000 at 0.0028 = 280.00; PLN 1,000 at 0.25 = 250.00.
    const fx = mobile
      ? { ccy: 'PLN', balance: '1000.00', rate: '0.25', total: '1,250.00', priv: 'RON' }
      : { ccy: 'HUF', balance: '100000.00', rate: '0.0028', total: '1,280.00', priv: 'BGN' }
    const householdID = await withQAClient(async (client) => {
      const r = await client.query(
        "INSERT INTO households (name, grace_period_days, created_at, updated_at) VALUES ('FX Household', 30, NOW(), NOW()) RETURNING id",
      )
      return Number(r.rows[0].id)
    })
    const user = await createThrowawayUser({ suite: `hhfx-${testInfo.project.name}`, householdID, role: 'owner' })
    const cookie = await apiSignin(user.email, user.password)
    const usd = await api(cookie, '/accounts', {
      name: 'HH Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD', balance: '1000.00',
    })
    const foreign = await api(cookie, '/accounts', {
      name: `HH ${fx.ccy} Savings`, institution_slug: 'manual', account_type: 'savings', currency: fx.ccy, balance: fx.balance,
    })
    await api(cookie, '/accounts', {
      name: `Private ${fx.priv} Stash`, institution_slug: 'manual', account_type: 'savings', currency: fx.priv, balance: '500.00',
    })
    await put(cookie, `/accounts/${usd.data.id}/shares/${householdID}`, { visibility: 'balance_only' })
    await put(cookie, `/accounts/${foreign.data.id}/shares/${householdID}`, { visibility: 'balance_only' })
    const before = (await get(cookie, '/h/dashboard')).data
    test.skip(before.net_worth_complete, `${fx.ccy} already has a fresh rate on this instance`)
    expect(before.net_worth_unpriced.map((u: { symbol: string }) => u.symbol)).toEqual([fx.ccy])

    await signIn(page, user.email, user.password)
    await page.goto('/h/insights')
    const band = page.locator('section', { hasText: 'Net worth' }).first()
    await expect(band).toContainText('1,000.00')
    await expect(band.getByText('partial', { exact: true }).first()).toBeVisible()
    await expect(band).toContainText(`Needs an exchange rate: ${fx.ccy} → USD`)
    // The private account's currency is never named in household scope.
    await expect(page.locator('body')).not.toContainText(fx.priv)
    const setRate = band.getByRole('button', { name: `Set exchange rate for ${fx.ccy}` })
    await expectInViewport(page, setRate)
    await expectNoOverflow(page)
    await testInfo.attach('h-insights-needs-rate', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    await setRate.click()
    const dialog = page.getByRole('dialog', { name: `Set exchange rate for ${fx.ccy}` })
    const rateInput = dialog.getByLabel(`1 ${fx.ccy} in USD`)
    await expect(rateInput).toBeFocused()
    await expectNoOverflow(page)
    await rateInput.fill(fx.rate)
    await testInfo.attach('h-set-rate', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dialog.getByRole('button', { name: 'Set rate' }).click()
    await expect(dialog).toHaveCount(0)

    await expect(band).toContainText(fx.total)
    await expect(band.getByText(/Needs an exchange rate/)).toHaveCount(0)
    await page.reload()
    await expect(page.locator('section', { hasText: 'Net worth' }).first()).toContainText(fx.total)
    await testInfo.attach('h-insights-complete', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    const after = (await get(cookie, '/h/dashboard')).data
    expect(after.net_worth_complete).toBe(true)
    expect(after.net_worth_unpriced).toEqual([])
    expect(Number(after.net_worth)).toBeCloseTo(Number(fx.total.replace(',', '')), 6)
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

async function put(cookie: string, path: string, body: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method: 'PUT', headers: { 'content-type': 'application/json', cookie }, body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
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
