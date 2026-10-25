import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Insights spending month stepper (#607): on Insights the user steps the
// "Spending by category" list and Top merchants back to last month for the
// month-end review, drills into that month's rows (the banner total equals
// the figure), and "This month" returns. Throwaway user; desktop and mobile.

test.describe('insights spending month', () => {
  test('step back to last month and drill into its spending', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `insights-month-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await call(cookie, 'GET', '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c as { id: number; name: string }
    }
    const acct = (await call(cookie, 'POST', '/accounts', {
      name: 'Month Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number

    const now = new Date()
    const ymd = (monthOffset: number, d: number) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, d)).toISOString().slice(0, 10)
    const label = (monthOffset: number) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, 1))
        .toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
    const tx = (amount: string, description: string, date: string, category_id?: number, merchant_name?: string) =>
      call(cookie, 'POST', '/transactions', {
        account_id: acct, amount, description, transaction_date: date, source: 'csv', category_id, merchant_name,
      })
    const travel = cat('travel')
    const groceries = cat('groceries')
    // Last month: Travel 410.25 at Month Airline, Groceries 150 - 20 refund = 130
    // at Month Grocer, plus salary (income stays out). This month: 40 groceries.
    await tx('3200.00', 'Month Salary', ymd(-1, 1), cat('income').id)
    await tx('-410.25', 'MONTH AIRLINE 123', ymd(-1, 8), travel.id, 'Month Airline')
    await tx('-150.00', 'Month Grocer', ymd(-1, 10), groceries.id, 'Month Grocer')
    await tx('20.00', 'Month Grocer refund', ymd(-1, 12), groceries.id, 'Month Grocer')
    await tx('-40.00', 'Month Grocer today', ymd(0, 1), groceries.id, 'Month Grocer')

    await signIn(page, user.email, user.password)
    await page.goto('/insights')
    const stepper = page.getByRole('navigation', { name: 'Spending month' })
    const band = page.locator('section', { has: stepper })
    const row = (name: string) => band.getByRole('listitem').filter({ has: page.getByRole('link', { name, exact: true }) })
    const merchants = band.locator('div', { has: page.getByRole('heading', { name: /^Top merchants/ }) }).last()

    await expect(band.getByRole('heading', { name: `Spending by category · ${label(0)}` })).toBeVisible()
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeDisabled()
    await expect(row(groceries.name)).toContainText('40.00')
    await expect(row(travel.name)).toHaveCount(0)

    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(band.getByRole('heading', { name: `Spending by category · ${label(-1)}` })).toBeVisible()
    await expect(row(travel.name)).toContainText('410.25')
    await expect(row(groceries.name)).toContainText('130.00')
    await expect(band.getByRole('link', { name: 'Income', exact: true })).toHaveCount(0)
    await expect(merchants.getByRole('heading', { name: `Top merchants · ${label(-1)}` })).toBeVisible()
    await expect(merchants.getByRole('listitem').filter({ hasText: 'Month Airline' })).toContainText('410.25')
    await expect(merchants.getByRole('listitem').filter({ hasText: 'Month Grocer' })).toContainText('130.00')
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeEnabled()
    await band.scrollIntoViewIfNeeded()
    await testInfo.attach(`${testInfo.project.name}-insights-last-month`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await expectNoClipping(page, 'insights last month')

    // Last month's Groceries opens exactly last month's two rows.
    await row(groceries.name).getByRole('link', { name: groceries.name }).click()
    const banner = page.getByRole('region', { name: 'Spending drill-down' })
    await expect(banner.getByTestId('spend-total')).toContainText('130.00')
    await expect(banner).toContainText('2 transactions')
    await expect(page.getByText('Month Grocer today')).toHaveCount(0)
    await testInfo.attach(`${testInfo.project.name}-insights-last-month-drill`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // The merchant figure opens the same month too.
    await page.goBack()
    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(merchants.getByRole('listitem').filter({ hasText: 'Month Airline' })).toContainText('410.25')
    await merchants.getByRole('link', { name: 'Month Airline' }).click()
    await expect(banner.getByTestId('spend-total')).toContainText('410.25')
    await expect(banner).toContainText('1 transaction')

    // Two months back is empty and says so; "This month" returns.
    await page.goto('/insights')
    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(band.getByText(`No spending in ${label(-2)}.`)).toBeVisible()
    await expect(merchants.getByText(`No merchant spend in ${label(-2)}.`)).toBeVisible()
    await stepper.getByRole('button', { name: 'This month' }).click()
    await expect(band.getByRole('heading', { name: `Spending by category · ${label(0)}` })).toBeVisible()
    await expect(row(groceries.name)).toContainText('40.00')
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeDisabled()
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
