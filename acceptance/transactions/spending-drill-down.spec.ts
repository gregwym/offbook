import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Spending drill-down (#571): a category, merchant or budget figure opens
// exactly the transactions behind it, and the banner total equals the
// figure to the cent — refunds net, a split counts only its share, hidden
// rows and transfers stay out. Throwaway user; desktop and mobile projects.

test.describe('spending drill-down', () => {
  test('category, merchant and budget figures open the rows behind them', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `drill-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await call(cookie, 'GET', '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c.id as number
    }
    const mk = async (name: string) =>
      (await call(cookie, 'POST', '/accounts', { name, institution_slug: 'manual', account_type: 'checking', currency: 'USD' })).data.id as number
    const a = await mk('Drill Checking')
    const b = await mk('Drill Savings')

    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    const tx = async (account_id: number, amount: string, description: string, d: number, category_id?: number, merchant_name?: string) =>
      (await call(cookie, 'POST', '/transactions', {
        account_id, amount, description, transaction_date: day(d), source: 'csv', category_id, merchant_name,
      })).data.id as number
    await tx(a, '3200.00', 'Drill Salary', 1, cat('income'))
    await tx(a, '-100.00', 'Drill Grocer', 2, cat('groceries'), 'Drill Grocer')
    await tx(a, '30.00', 'Drill Grocer refund', 3, cat('groceries'), 'Drill Grocer')
    const split = await tx(a, '-80.00', 'Drill Superstore', 4, cat('gas'), 'Drill Superstore')
    await call(cookie, 'PUT', `/transactions/${split}/splits`, {
      splits: [
        { category_id: cat('groceries'), amount: '-50.00' },
        { category_id: cat('gas'), amount: '-30.00' },
      ],
    })
    const hidden = await tx(a, '-40.00', 'Drill Reimbursed lunch', 5, cat('groceries'), 'Drill Grocer')
    await call(cookie, 'POST', '/transactions/bulk-edit', { ids: [hidden], hidden: true })
    await tx(a, '-500.00', 'Drill Move out', 6)
    await tx(b, '500.00', 'Drill Move in', 6)
    await call(cookie, 'POST', '/budgets', { category_id: cat('groceries'), period: 'monthly', amount: '200.00' })

    // Groceries = 100 - 30 refund + 50 split share = 120.00.
    await signIn(page, user.email, user.password)
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    const groceries = band.getByRole('listitem').filter({ hasText: 'Groceries' }).first()
    await expect(groceries).toContainText('120.00')
    await groceries.getByRole('link', { name: 'Groceries' }).click()

    await expect(page).toHaveURL(/\/transactions\?.*spending=1/)
    const banner = page.getByRole('region', { name: 'Spending drill-down' })
    await expect(banner).toContainText('Groceries')
    await expect(banner).toContainText('3 transactions')
    await expect(banner.getByTestId('spend-total')).toContainText('120.00')
    // Rows render a phone and a desktop layout; only the visible one counts.
    for (const d of ['Drill Grocer refund', 'Drill Superstore']) {
      await expect(page.getByText(d).filter({ visible: true }).first()).toBeVisible()
    }
    await expect(page.getByText('Drill Reimbursed lunch')).toHaveCount(0)
    await expect(page.getByText('Drill Salary')).toHaveCount(0)
    await expect(page.getByText('Drill Move out')).toHaveCount(0)
    await testInfo.attach(`${testInfo.project.name}-category-drill-down`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await expectNoClipping(page, 'category drill-down')

    // A reload keeps the drill-down; "Show all transactions" leaves it.
    await page.reload()
    await expect(banner.getByTestId('spend-total')).toContainText('120.00')
    await banner.getByRole('button', { name: 'Show all transactions' }).click()
    await expect(banner).toHaveCount(0)
    await expect(page).not.toHaveURL(/spending=1/)
    await expect(page.getByText('Drill Salary').filter({ visible: true }).first()).toBeVisible()

    // Top merchants: Drill Grocer = 100 - 30 = 70.00 (the hidden 40 stays out).
    await page.goto('/insights')
    const merchants = band.locator('div', { has: page.getByRole('heading', { name: 'Top merchants' }) }).last()
    const grocer = merchants.getByRole('listitem').filter({ hasText: 'Drill Grocer' })
    await expect(grocer).toContainText('70.00')
    await grocer.getByRole('link', { name: 'Drill Grocer' }).click()
    await expect(banner).toContainText('Drill Grocer')
    await expect(banner).toContainText('2 transactions')
    await expect(banner.getByTestId('spend-total')).toContainText('70.00')

    // The Budgets page's spent figure opens the same 120.00.
    await page.goto('/budgets')
    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: 'Groceries' }) }).first()
    await expect(card).toContainText('120.00')
    await card.getByRole('link', { name: /see transactions/i }).click()
    await expect(banner.getByTestId('spend-total')).toContainText('120.00')
    await expect(banner).toContainText('3 transactions')
    await testInfo.attach(`${testInfo.project.name}-budget-drill-down`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await expectNoClipping(page, 'budget drill-down')
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function call(cookie: string, method: string, path: string, body?: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method,
    headers: { 'content-type': 'application/json', cookie },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${method} ${path} ${res.status} ${await res.text()}`)
  return res.json()
}

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill(email)
  await page.getByLabel(/password/i).fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/h\/insights|\/accounts/)
}
