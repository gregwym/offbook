import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Budget history (#577): the Budgets page steps back to last month and shows
// each monthly budget's actual for that month (refunds net), its "See
// transactions" link opens the same total, and "next" stops at the current
// month. Throwaway user; desktop and mobile projects.

test.describe('budget history', () => {
  test('step back to last month and see that month against the budget', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `budget-history-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await call(cookie, 'GET', '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c as { id: number; name: string }
    }
    const acct = (await call(cookie, 'POST', '/accounts', {
      name: 'History Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number

    const now = new Date()
    const ymd = (monthOffset: number, d: number) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, d)).toISOString().slice(0, 10)
    const label = (monthOffset: number) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, 1))
        .toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
    const tx = (amount: string, description: string, date: string, category_id: number) =>
      call(cookie, 'POST', '/transactions', {
        account_id: acct, amount, description, transaction_date: date, source: 'csv', category_id,
      })
    const groceries = cat('groceries')
    const gas = cat('gas')
    // Last month: 150 - 20 refund = 130. This month: 40.
    await tx('-150.00', 'History Grocer', ymd(-1, 10), groceries.id)
    await tx('20.00', 'History Grocer refund', ymd(-1, 12), groceries.id)
    await tx('-40.00', 'History Grocer today', ymd(0, 1), groceries.id)
    await call(cookie, 'POST', '/budgets', { category_id: groceries.id, period: 'monthly', amount: '200.00' })
    await call(cookie, 'POST', '/budgets', { category_id: gas.id, period: 'weekly', amount: '25.00' })

    await signIn(page, user.email, user.password)
    await page.goto('/budgets')
    const stepper = page.getByRole('navigation', { name: 'Budget month' })
    const card = (name: string) =>
      page.locator('div.rounded-lg', { has: page.getByRole('heading', { name, exact: true }) }).last()
    await expect(stepper).toContainText(label(0))
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeDisabled()
    await expect(card(groceries.name)).toContainText('40.00')
    await expect(card(gas.name)).toBeVisible()

    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(stepper).toContainText(label(-1))
    const g = card(groceries.name)
    await expect(g).toContainText('130.00')
    await expect(g).toContainText('65%')
    await expect(g).toContainText('70.00 USD remaining')
    // The period reads as the month's first and last day.
    await expect(g).toContainText(`${ymd(-1, 1)} → ${ymd(0, 0)}`)
    // The weekly budget doesn't line up with a calendar month.
    await expect(page.getByRole('heading', { name: gas.name, exact: true })).toHaveCount(0)
    await expect(page.getByText(/1 weekly, annual or paused budget is only shown for the current period/)).toBeVisible()
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeEnabled()
    await testInfo.attach(`${testInfo.project.name}-budgets-last-month`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await expectNoClipping(page, 'budgets last month')

    // Last month's figure opens exactly last month's rows.
    await g.getByRole('link', { name: /see transactions/i }).click()
    const banner = page.getByRole('region', { name: 'Spending drill-down' })
    await expect(banner.getByTestId('spend-total')).toContainText('130.00')
    await expect(banner).toContainText('2 transactions')
    await expect(page.getByText('History Grocer today')).toHaveCount(0)

    // Back on Budgets, two steps back is an empty month; "This month" returns.
    await page.goto('/budgets')
    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(stepper).toContainText(label(-2))
    await expect(card(groceries.name)).toContainText('0.00 USD')
    await expect(card(groceries.name)).toContainText('200.00 USD remaining')
    await stepper.getByRole('button', { name: 'This month' }).click()
    await expect(stepper).toContainText(label(0))
    await expect(card(groceries.name)).toContainText('40.00')
    await expect(card(gas.name)).toBeVisible()
    await testInfo.attach(`${testInfo.project.name}-budgets-this-month`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await expectNoClipping(page, 'budgets this month')
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
