import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Browser regression for the Insights spending band (#456): ordinary spend
// must render as a positive amount with a bar, refunds net against their
// category, and an unconfirmed transfer pair must not count as spending.
// Throwaway user; runs under both desktop and mobile projects.

test.describe('insights spending band', () => {
  test('shows positive spend, nets refunds, excludes transfers', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `spending-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c.id as number
    }
    const mk = (name: string) =>
      api(cookie, '/accounts', { name, institution_slug: 'manual', account_type: 'checking', currency: 'USD' })
    const a = (await mk('Spend Checking')).data.id
    const b = (await mk('Spend Savings')).data.id

    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    const tx = (account_id: number, amount: string, description: string, d: number, category_id?: number) =>
      api(cookie, '/transactions', {
        account_id, amount, description, transaction_date: day(d), source: 'csv', category_id,
      })
    await tx(a, '3200.00', 'QA Salary', 1, cat('income'))
    await tx(a, '-100.00', 'QA Grocer', 2, cat('groceries'))
    await tx(a, '30.00', 'QA Grocer refund', 3, cat('groceries'))
    await tx(a, '-60.00', 'QA Gas station', 4, cat('gas'))
    await tx(a, '-500.00', 'QA Move out', 5)
    await tx(b, '500.00', 'QA Move in', 5)

    await signIn(page, user.email, user.password)
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    await expect(band).toBeVisible()
    // Groceries 100 - 30 refund = 70; Gas = 60. Neither is a "net refund".
    const groceries = band.getByRole('listitem').filter({ hasText: 'Groceries' }).first()
    const gas = band.getByRole('listitem').filter({ hasText: 'Gas' }).first()
    await expect(groceries).toContainText('70.00')
    await expect(gas).toContainText('60.00')
    await expect(band.getByText('net refund')).toHaveCount(0)
    await expect(band).not.toContainText('-70.00')
    // Spending bars render with a non-zero width.
    const bar = groceries.locator('div.bg-indigo-400')
    const w = await bar.evaluate((el) => (el as HTMLElement).getBoundingClientRect().width)
    expect(w, 'groceries bar has width').toBeGreaterThan(0)
    // The unconfirmed 500 transfer pair is not spending.
    await expect(page.getByText(/spending:/i).first()).toContainText('130.00')
    await expect(page.getByText(/spending:/i).first()).not.toContainText('630.00')
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
