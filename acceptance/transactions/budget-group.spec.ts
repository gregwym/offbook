import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Group budgets (#596). A budget on a category group counts the spending in
// its subcategories, except a subcategory with its own budget, which counts
// only there. The card names what it includes, its "See transactions" opens
// the whole group with the same total, and the month totals count each
// dollar once. Throwaway user; desktop and mobile projects.

test.describe('group budgets', () => {
  test('a group budget counts its subcategories and drills into all of them', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `budget-group-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const category = async (name: string, parent?: number) =>
      (await call(cookie, 'POST', '/categories', { name, parent_id: parent ?? null })).data.id as number
    const food = await category('Food Group')
    const market = await category('Market Run', food)
    const takeout = await category('Takeout Night', food)
    const coffee = await category('Coffee Stops', food)
    const acct = (await call(cookie, 'POST', '/accounts', {
      name: 'Group Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = async (amount: string, description: string, category: number) =>
      call(cookie, 'POST', '/transactions', {
        account_id: acct, amount, description, transaction_date: day, source: 'csv', category_id: category,
      })
    await tx('-40.00', 'Group Farmers market', market)
    await tx('-12.50', 'Group Noodle bar', takeout)
    await tx('-8.00', 'Group Espresso', coffee)
    await call(cookie, 'POST', '/budgets', { category_id: food, period: 'monthly', amount: '100.00' })
    await call(cookie, 'POST', '/budgets', { category_id: coffee, period: 'monthly', amount: '20.00' })

    // API: coffee counts in its own budget only, so nothing is double counted.
    const sum = (await call(cookie, 'GET', '/budgets/summary')).data
    expect(Number(sum.spent_in_budgets)).toBe(60.5)
    expect(Number(sum.not_in_budget)).toBe(0)
    expect(Number(sum.total_spending)).toBe(60.5)

    await signIn(page, user.email, user.password)
    await page.goto('/budgets')
    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: 'Food Group', exact: true }) }).last()
    await expect(card).toContainText('52.50')
    await expect(card).toContainText('of 100.00')
    await expect(card.getByText(/^Includes /)).toHaveText(/Market Run/)
    await expect(card.getByText(/^Includes /)).toHaveText(/Takeout Night/)
    await expect(card.getByText(/^Includes /)).not.toHaveText(/Coffee Stops/)
    const coffeeCard = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: 'Coffee Stops', exact: true }) }).last()
    await expect(coffeeCard).toContainText('8.00')
    await expect(page.getByTestId('total-spent')).toContainText('60.50')
    await expect(page.getByTestId('total-unbudgeted')).toContainText('0.00')
    await expectNoClipping(page, 'group budget card')
    await testInfo.attach(`${testInfo.project.name}-group-budget`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await card.getByRole('link', { name: /see transactions/i }).click()
    await expect(page).toHaveURL(/\/transactions\?.*subcategory_ids=/)
    const banner = page.getByRole('region', { name: 'Spending drill-down' })
    await expect(banner).toContainText('Food Group (incl.')
    await expect(banner.getByTestId('spend-total')).toContainText('52.50')
    await expect(page.getByText('Group Farmers market').first()).toBeVisible()
    await expect(page.getByText('Group Noodle bar').first()).toBeVisible()
    await expect(page.getByText('Group Espresso')).toHaveCount(0)
    await testInfo.attach(`${testInfo.project.name}-group-drill`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
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
