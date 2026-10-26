import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Insights spending month stepper (#607): on Insights the user steps the
// "Spending by category" list and Top merchants back to last month for the
// month-end review, drills into that month's rows (the banner total equals
// the figure), and "This month" returns. The month's income, spending and net
// (#609) follow the stepper; spending equals the category rows and drills in.
// The "vs. average" category trend (#611) follows the stepper as well: each
// figure equals that month's category row. Once stepped back, no figure on the
// page still shows this month's income or spending (#617), and the footer reads
// as an inclusive range. Throwaway user; desktop and mobile.

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
    const trend = (monthOffset: number) =>
      band.locator('div', { has: page.getByRole('heading', { name: `${label(monthOffset)} vs. average` }) }).last()
    const trendRow = (monthOffset: number, name: string) => trend(monthOffset).getByRole('listitem').filter({ hasText: name })

    await expect(band.getByRole('heading', { name: `Spending by category · ${label(0)}` })).toBeVisible()
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeDisabled()
    await expect(row(groceries.name)).toContainText('40.00')
    // #617: the footer is an inclusive human range, not the raw [from, to) window.
    const day = (monthOffset: number, d: number, year: boolean) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, d))
        .toLocaleString('en-US', { month: 'short', day: 'numeric', ...(year ? { year: 'numeric' } : {}), timeZone: 'UTC' })
    await expect(page.getByText(`This period: ${day(0, 1, false)} – ${day(1, 0, true)}`)).toBeVisible()
    await expect(page.getByText(/Income \(period\)/)).toHaveCount(0)
    await expect(row(travel.name)).toHaveCount(0)
    // This month: nothing in yet, 40 out (#609).
    const totals = (monthOffset: number) => band.getByLabel(`${label(monthOffset)} totals`)
    await expect(totals(0)).toContainText('Income0.00 USD')
    await expect(totals(0)).toContainText('Spending40.00 USD')
    await expect(totals(0)).toContainText('Net-40.00 USD')
    await expect(totals(0)).toContainText('Spent more than came in')
    // 40 this month vs. last month's 130 averaged over the 5 months before (26).
    await expect(trendRow(0, groceries.name)).toContainText('40.00')
    await expect(trendRow(0, groceries.name)).toContainText('▲ 54%')

    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(band.getByRole('heading', { name: `Spending by category · ${label(-1)}` })).toBeVisible()
    await expect(row(travel.name)).toContainText('410.25')
    await expect(row(groceries.name)).toContainText('130.00')
    await expect(band.getByRole('link', { name: 'Income', exact: true })).toHaveCount(0)
    await expect(merchants.getByRole('heading', { name: `Top merchants · ${label(-1)}` })).toBeVisible()
    await expect(merchants.getByRole('listitem').filter({ hasText: 'Month Airline' })).toContainText('410.25')
    await expect(merchants.getByRole('listitem').filter({ hasText: 'Month Grocer' })).toContainText('130.00')
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeEnabled()
    // Last month's totals: salary in, 410.25 + 130.00 out, the rest kept.
    await expect(totals(-1)).toContainText('Income3,200.00 USD')
    await expect(totals(-1)).toContainText('Spending540.25 USD')
    await expect(totals(-1)).toContainText('Net2,659.75 USD')
    await expect(totals(-1)).toContainText('Kept after spending')
    // #617: nothing on the page still shows this month's 40.00 spending or a
    // second income/spending pair — the month tiles are the only cash-flow figures.
    await expect(page.getByText(/(^|[^\d,.])40\.00 USD/).filter({ visible: true })).toHaveCount(0)
    await expect(page.getByText(/Income \(period\)/)).toHaveCount(0)
    // The trend follows the stepper (#611): last month's figures, not this month's.
    await expect(trend(0)).toHaveCount(0)
    await expect(trendRow(-1, travel.name)).toContainText('410.25')
    await expect(trendRow(-1, groceries.name)).toContainText('130.00')
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

    // Last month's spending total opens every row behind it, and only those.
    await page.goBack()
    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await totals(-1).getByRole('link', { name: '540.25 USD' }).click()
    await expect(banner.getByTestId('spend-total')).toContainText('540.25')
    await expect(banner).toContainText('3 transactions')
    await expect(page.getByText('Month Salary')).toHaveCount(0)
    await testInfo.attach(`${testInfo.project.name}-insights-last-month-total-drill`, {
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
    await expect(totals(-2)).toHaveCount(0)
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
