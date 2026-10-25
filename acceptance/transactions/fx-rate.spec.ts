import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for a foreign-currency account with no exchange rate (#599, Daily
// Driver gate 4). A USD user also holds cash in a currency with no FX rate on
// file. Insights must flag net worth partial, name the currency ("Needs an
// exchange rate: ISK → USD"), and open Set rate for that currency; once the
// rate is saved, net worth is complete and correct to the cent. Rates are
// instance-wide reference data, so each project uses its own currency.
// Runs under desktop and mobile.

test.describe('missing exchange rate', () => {
  test('partial net worth names the currency and Set rate completes it', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const mobile = testInfo.project.name.startsWith('mobile')
    // ISK 100,000 at 0.0072 = 720.00; CZK 10,000 at 0.043 = 430.00.
    const fx = mobile
      ? { ccy: 'CZK', balance: '10000.00', rate: '0.043', total: '1,430.00' }
      : { ccy: 'ISK', balance: '100000.00', rate: '0.0072', total: '1,720.00' }
    const user = await createThrowawayUser({ suite: `fx-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await api(cookie, '/accounts', {
      name: 'FX Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD', balance: '1000.00',
    })
    await api(cookie, '/accounts', {
      name: `FX ${fx.ccy} Savings`, institution_slug: 'manual', account_type: 'savings', currency: fx.ccy, balance: fx.balance,
    })
    const before = (await get(cookie, '/dashboard/summary')).data
    test.skip(before.net_worth_complete, `${fx.ccy} already has a fresh rate on this instance`)

    await signIn(page, user.email, user.password)
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Net worth' }).first()
    // Only the USD cash counts; the foreign balance is left out and named.
    await expect(band).toContainText('1,000.00')
    await expect(band.getByText('partial', { exact: true }).first()).toBeVisible()
    await expect(band).toContainText(`Needs an exchange rate: ${fx.ccy} → USD`)
    const setRate = band.getByRole('button', { name: `Set exchange rate for ${fx.ccy}` })
    await expectInViewport(page, setRate)
    await expectNoOverflow(page)
    await testInfo.attach('insights-needs-rate', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    await setRate.click()
    const dialog = page.getByRole('dialog', { name: `Set exchange rate for ${fx.ccy}` })
    await expect(dialog).toContainText(`Enter what 1 ${fx.ccy} is worth in USD today.`)
    const rateInput = dialog.getByLabel(`1 ${fx.ccy} in USD`)
    await expect(rateInput).toBeFocused()
    await expectNoOverflow(page)
    await rateInput.fill(fx.rate)
    await testInfo.attach('set-rate', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dialog.getByRole('button', { name: 'Set rate' }).click()
    await expect(dialog).toHaveCount(0)

    await expect(band).toContainText(fx.total)
    await expect(band.getByText(/Needs an exchange rate/)).toHaveCount(0)
    await page.reload()
    await expect(page.locator('section', { hasText: 'Net worth' }).first()).toContainText(fx.total)
    await testInfo.attach('insights-complete', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    const after = (await get(cookie, '/dashboard/summary')).data
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
