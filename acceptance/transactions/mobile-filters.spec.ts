import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// #518: on a phone the Transactions filter stack used to fill the first
// screen. Below md only Search and a "Filters" toggle show up front; the list
// must start inside the first viewport, and the hidden filters must still be
// reachable and work. Desktop keeps all five filters inline with no toggle.
// #702: on desktop Search is wide enough for its placeholder, and the pager
// only appears when there is a second page.

test.describe('transactions filters journey', () => {
  test('list stays above the fold; secondary filters reachable and working', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `txfilters-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const account = async (name: string) => (await api(cookie, '/accounts', {
      name, institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const checking = await account('Filter Checking')
    const savings = await account('Filter Savings')
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (account_id: number, description: string, amount: string) =>
      api(cookie, '/transactions', { account_id, amount, description, transaction_date: day, source: 'csv' })
    await tx(checking, 'QAFILT Corner Cafe', '-4.50')
    await tx(savings, 'QAFILT Interest', '1.25')

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    const cafe = page.getByText('QAFILT Corner Cafe').locator('visible=true').first()
    const interest = page.getByText('QAFILT Interest').locator('visible=true').first()
    await expect(cafe).toBeVisible()
    await expect(page.getByPlaceholder('description or merchant')).toBeVisible()

    const vp = page.viewportSize()!
    const toggle = page.getByRole('button', { name: /^Filters/ })
    const fromInput = page.getByLabel('From', { exact: true })
    const accountSelect = page.getByRole('combobox', { name: /^Account/ })

    if (vp.width < 768) {
      // First transaction is inside the first screen, without scrolling.
      const box = (await cafe.boundingBox())!
      expect(box.y + box.height, 'first transaction inside the first viewport').toBeLessThanOrEqual(vp.height)
      await expect(fromInput).toBeHidden()
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      await testInfo.attach(`${testInfo.project.name}-filters-collapsed`, { body: await page.screenshot(), contentType: 'image/png' })

      await toggle.click()
      await expect(toggle).toHaveAttribute('aria-expanded', 'true')
      await expect(fromInput).toBeVisible()
    } else {
      await expect(toggle).toBeHidden()
      await expect(fromInput).toBeVisible()
      // #702: Search spans two columns so its placeholder isn't clipped.
      const search = page.getByPlaceholder('description or merchant')
      const fits = await search.evaluate((el) => {
        const input = el as HTMLInputElement
        const ctx = document.createElement('canvas').getContext('2d')!
        const cs = getComputedStyle(input)
        ctx.font = `${cs.fontSize} ${cs.fontFamily}`
        const pad = parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight)
        return ctx.measureText(input.placeholder).width + pad <= input.clientWidth
      })
      expect(fits, 'search placeholder fits its input').toBe(true)
    }
    // #702: two rows fit on one page, so there is no pager.
    await expect(page.getByRole('navigation', { name: 'Pagination' })).toHaveCount(0)

    // The revealed filters work: narrowing to one account hides the other's row.
    await accountSelect.selectOption({ label: 'Filter Savings' })
    await expect(interest).toBeVisible()
    await expect(page.getByText('QAFILT Corner Cafe').locator('visible=true')).toHaveCount(0)
    if (vp.width < 768) {
      await expect(toggle).toContainText('· 1')
      // #554: folding the panel leaves a removable chip for the live filter,
      // and the filtered list starts on the first screen.
      await toggle.click()
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      await expect(page.getByRole('button', { name: 'Remove filter Account: Filter Savings' })).toBeVisible()
      const box = (await interest.boundingBox())!
      expect(box.y + box.height, 'filtered row inside the first viewport').toBeLessThanOrEqual(vp.height)
      await testInfo.attach(`${testInfo.project.name}-filter-chip`, { body: await page.screenshot(), contentType: 'image/png' })
    }
    await testInfo.attach(`${testInfo.project.name}-filters-applied`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    // Search stays up front and combines with the account filter.
    await page.getByPlaceholder('description or merchant').fill('nomatch-zzz')
    await expect(page.getByText('No transactions match these filters.')).toBeVisible()
    // #702: an empty result has no Prev/Next noise under it.
    await expect(page.getByRole('navigation', { name: 'Pagination' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Next', exact: true })).toHaveCount(0)
    await testInfo.attach(`${testInfo.project.name}-empty-no-pager`, { body: await page.screenshot(), contentType: 'image/png' })
    await page.getByRole('button', { name: /clear filters/i }).click()
    await expect(cafe).toBeVisible()
    await expect(interest).toBeVisible()
    await expectNoOverflow(page)
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
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
