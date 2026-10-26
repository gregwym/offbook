import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Budget month totals (#589). The Budgets page answers three questions for
// the month: how much was budgeted, how much was spent against it, and how
// much was spent outside any budget. Spent in budgets + not in a budget is
// the Insights spending figure, refunds net, hidden rows and transfers stay
// out, and a weekly budget sits apart under "Other periods". Throwaway user;
// desktop and mobile projects.

test.describe('budget month totals', () => {
  test('budgeted, spent, remaining and not-in-a-budget add up to Insights spending', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `budget-summary-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await call(cookie, 'GET', '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c as { id: number; name: string }
    }
    const acct = (await call(cookie, 'POST', '/accounts', {
      name: 'Totals Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const label = now.toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
    const tx = async (amount: string, description: string, category?: number) =>
      (await call(cookie, 'POST', '/transactions', {
        account_id: acct, amount, description, transaction_date: day, source: 'csv', category_id: category,
      })).data.id as number
    const groceries = cat('groceries')
    const gas = cat('gas')
    const shopping = cat('shopping')
    await tx('-40.00', 'Totals Grocer', groceries.id)
    await tx('5.00', 'Totals Grocer refund', groceries.id)
    await tx('-120.10', 'Totals Shoe shop', shopping.id)
    await tx('-9.99', 'Totals Mystery charge')
    await tx('-12.00', 'Totals Fuel', gas.id)
    const hidden = await tx('-77.00', 'Totals Reimbursed', shopping.id)
    await call(cookie, 'POST', '/transactions/bulk-edit', { ids: [hidden], hidden: true })
    await call(cookie, 'POST', '/budgets', { category_id: groceries.id, period: 'monthly', amount: '300.00' })
    await call(cookie, 'POST', '/budgets', { category_id: gas.id, period: 'weekly', amount: '25.00' })

    // API: the identity holds to the cent. Gas has only a weekly budget, so
    // its 12.00 is outside the month's budgets.
    const sum = (await call(cookie, 'GET', '/budgets/summary')).data
    expect(sum).toMatchObject({ budget_count: 1 })
    expect(Number(sum.budgeted)).toBe(300)
    expect(Number(sum.spent_in_budgets)).toBe(35)
    expect(Number(sum.not_in_budget)).toBe(142.09)
    expect(Number(sum.total_spending)).toBe(177.09)

    await signIn(page, user.email, user.password)
    await page.goto('/insights')
    await expect(page.getByLabel(/^[A-Z][a-z]+ \d{4} totals$/)).toContainText('Spending177.09')

    await page.goto('/budgets')
    const strip = page.getByRole('region', { name: `${label} totals` })
    await expect(strip.getByTestId('total-budgeted')).toContainText('300.00')
    await expect(strip.getByTestId('total-spent')).toContainText('35.00')
    await expect(strip.getByTestId('total-remaining')).toContainText('Remaining')
    await expect(strip.getByTestId('total-remaining')).toContainText('265.00')
    await expect(strip.getByTestId('total-unbudgeted')).toContainText('142.09')
    await expect(strip).toContainText(`All spending in ${label}: 177.09 USD`)
    // The strip matches the Groceries card.
    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: groceries.name, exact: true }) }).last()
    await expect(card).toContainText('35.00')
    await expect(card).toContainText('of 300.00')
    // The weekly budget sits apart and isn't in the month's figures.
    const other = page.getByRole('region', { name: 'Other periods' })
    await expect(other.getByRole('heading', { name: gas.name, exact: true })).toBeVisible()
    await expect(other).toContainText('aren’t part of this month’s totals')

    // Phone: the four figures wrap 2×2 inside the strip, nothing side-scrolls.
    const box = (await strip.boundingBox())!
    const vw = page.viewportSize()!.width
    expect(box.x + box.width, 'strip inside the viewport').toBeLessThanOrEqual(vw)
    const tiles = await Promise.all(['total-budgeted', 'total-spent', 'total-remaining', 'total-unbudgeted']
      .map(async (id) => (await strip.getByTestId(id).boundingBox())!))
    for (const t of tiles) expect(t.x + t.width, 'tile inside the strip').toBeLessThanOrEqual(box.x + box.width + 1)
    const rows = new Set(tiles.map((t) => Math.round(t.y)))
    expect(rows.size, 'tiles per row').toBe(vw < 768 ? 2 : 1)
    await expectNoClipping(page, 'budgets totals')

    // Where the money outside budgets went: largest first, each linked.
    await strip.getByText('Where the spending outside budgets went').click()
    const items = strip.getByRole('listitem')
    await expect(items).toHaveCount(3)
    await expect(items.nth(0)).toContainText(shopping.name)
    await expect(items.nth(0)).toContainText('120.10')
    await expect(items.filter({ hasText: 'Uncategorized' })).toContainText('9.99')
    await expect(items.filter({ hasText: gas.name })).toContainText('12.00')
    await expect(items.filter({ hasText: gas.name })).toContainText('Has a weekly budget')
    await testInfo.attach(`${testInfo.project.name}-budget-totals`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await strip.getByRole('link', { name: `See ${shopping.name} transactions` }).click()
    await expect(page).toHaveURL(/\/transactions\?.*spending=1/)
    const banner = page.getByRole('region', { name: 'Spending drill-down' })
    await expect(banner.getByTestId('spend-total')).toContainText('120.10')
    await expect(page.getByText('Totals Reimbursed')).toHaveCount(0)
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
