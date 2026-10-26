import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser, withQAClient } from '../fixtures/state.mjs'

// Household Insights month stepper (#619): a member steps the shared
// spending band back to last month and reads the same Income / Spending /
// Net tiles, month titles and category rows as personal Insights, from
// shared balance_and_txns accounts only — a balance_only and a private
// account's spending never shows. Household figures are aggregates, so no
// figure is a drill-through link. Throwaway household; desktop and mobile.

test.describe('household insights spending month', () => {
  test('step back to last month: tiles and rows equal the shared rows', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const householdID = await withQAClient(async (client) => {
      const r = await client.query(
        "INSERT INTO households (name, grace_period_days, created_at, updated_at) VALUES ('Month Household', 30, NOW(), NOW()) RETURNING id",
      )
      return Number(r.rows[0].id)
    })
    const user = await createThrowawayUser({ suite: `hhmonth-${testInfo.project.name}`, householdID, role: 'owner' })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await call(cookie, 'GET', '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c as { id: number; name: string }
    }
    const account = async (name: string) =>
      (await call(cookie, 'POST', '/accounts', {
        name, institution_slug: 'manual', account_type: 'checking', currency: 'USD',
      })).data.id as number
    const shared = await account('HH Month Joint')
    const balOnly = await account('HH Month Savings')
    const priv = await account('HH Month Private')
    await call(cookie, 'PUT', `/accounts/${shared}/shares/${householdID}`, { visibility: 'balance_and_txns' })
    await call(cookie, 'PUT', `/accounts/${balOnly}/shares/${householdID}`, { visibility: 'balance_only' })

    const now = new Date()
    const ymd = (monthOffset: number, d: number) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, d)).toISOString().slice(0, 10)
    const label = (monthOffset: number) =>
      new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + monthOffset, 1))
        .toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
    const tx = (account_id: number, amount: string, description: string, date: string, category_id: number) =>
      call(cookie, 'POST', '/transactions', { account_id, amount, description, transaction_date: date, source: 'csv', category_id })
    const groceries = cat('groceries')
    const travel = cat('travel')
    // Last month on the joint account: 2,000.00 in; 150.40 groceries and
    // 60.00 travel out. This month: 25.00 groceries.
    await tx(shared, '2000.00', 'HH Month Salary', ymd(-1, 1), cat('income').id)
    await tx(shared, '-150.40', 'HH Month Grocer', ymd(-1, 9), groceries.id)
    await tx(shared, '-60.00', 'HH Month Train', ymd(-1, 14), travel.id)
    await tx(shared, '-25.00', 'HH Month Grocer today', ymd(0, 1), groceries.id)
    // Never in household spending: balance_only and private accounts.
    await tx(balOnly, '-999.00', 'HH Month Hidden A', ymd(-1, 9), groceries.id)
    await tx(priv, '-777.00', 'HH Month Hidden B', ymd(-1, 9), groceries.id)

    await signIn(page, user.email, user.password)
    await page.goto('/h/insights')
    const stepper = page.getByRole('navigation', { name: 'Spending month' })
    const band = page.locator('section', { has: stepper })
    const totals = (monthOffset: number) => band.getByLabel(`${label(monthOffset)} totals`)
    const row = (name: string) => band.getByRole('listitem').filter({ hasText: name }).first()

    await expect(band.getByRole('heading', { name: `Spending by category · ${label(0)}` })).toBeVisible()
    await expect(stepper.getByRole('button', { name: 'Next month' })).toBeDisabled()
    await expect(totals(0)).toContainText('Spending25.00 USD')
    await expect(row(groceries.name)).toContainText('25.00')

    await stepper.getByRole('button', { name: 'Previous month' }).click()
    await expect(band.getByRole('heading', { name: `Spending by category · ${label(-1)}` })).toBeVisible()
    await expect(totals(-1)).toContainText('Income2,000.00 USD')
    await expect(totals(-1)).toContainText('Spending210.40 USD')
    await expect(totals(-1)).toContainText('Net1,789.60 USD')
    await expect(totals(-1)).toContainText('Kept after spending')
    await expect(row(groceries.name)).toContainText('150.40')
    await expect(row(travel.name)).toContainText('60.00')
    await expect(band.getByRole('heading', { name: `${label(-1)} vs. average` })).toBeVisible()
    // Shared balance_and_txns only, and spending, not signed sums.
    await expect(band).not.toContainText('999.00')
    await expect(band).not.toContainText('777.00')
    await expect(band).not.toContainText('net refund')
    // Aggregates: nothing in the band drills into a member's rows.
    await expect(band.getByRole('link')).toHaveCount(0)

    await band.scrollIntoViewIfNeeded()
    await testInfo.attach(`${testInfo.project.name}-household-insights-last-month`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await expectNoClipping(page, 'household insights last month')

    await stepper.getByRole('button', { name: 'This month' }).click()
    await expect(totals(0)).toContainText('Spending25.00 USD')
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
