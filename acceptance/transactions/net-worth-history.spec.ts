import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1 / #374 journey: Insights shows an exact net-worth
// headline for manual accounts with dated history, the range selector
// (3M/6M/1Y/3Y) refetches a longer series without breaking the headline,
// the choice survives nothing it shouldn't (reload resets to the default),
// earlier month-ends include the opening balance (#528), and neither
// viewport overflows. Throwaway user; desktop + mobile projects.

test.describe('net worth history journey', () => {
  test('headline is exact and the range selector extends the trend', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `networth-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const mk = (name: string, balance: string) =>
      apiPost(cookie, '/accounts', {
        name, institution_slug: 'manual', account_type: 'checking', currency: 'USD', balance,
      })
    const a = (await mk('NW Checking', '1000.10')).data.id as number
    const b = (await mk('NW Savings', '2500.55')).data.id as number

    const monthsAgo = (m: number) => {
      const d = new Date()
      d.setUTCDate(15)
      d.setUTCMonth(d.getUTCMonth() - m)
      return d.toISOString().slice(0, 10)
    }
    const tx = (account_id: number, amount: string, description: string, m: number) =>
      apiPost(cookie, '/transactions', {
        account_id, amount, description, transaction_date: monthsAgo(m), source: 'csv',
      })
    await tx(a, '-120.25', 'NW Rent top-up', 2)
    await tx(b, '300.00', 'NW Old bonus', 8)
    // Exact sum: 1000.10 + 2500.55 - 120.25 + 300.00 = 3680.40
    const expected = '3,680.40'

    await signIn(page, user.email, user.password)
    const trendResponse = (months: number) =>
      page.waitForResponse((r) => r.url().includes('/dashboard/net-worth') && r.url().includes(`months=${months}`))

    const first = trendResponse(12)
    await page.goto('/insights')
    await first
    const band = page.locator('section', { hasText: 'Net worth' }).first()
    await expect(band).toContainText(expected)
    await expect(band).not.toContainText('partial')
    const group = page.getByRole('group', { name: 'Net worth date range' })
    await expect(group.getByRole('button', { name: '1Y' })).toHaveAttribute('aria-pressed', 'true')

    const lens: Record<string, number> = {}
    let series: { date: string; total: string }[] = []
    for (const [label, months] of [['3M', 3], ['6M', 6], ['3Y', 36]] as const) {
      const resp = trendResponse(months)
      await group.getByRole('button', { name: label }).click()
      const body = await (await resp).json()
      lens[label] = body.data.length
      if (label === '3Y') series = body.data
      await expect(group.getByRole('button', { name: label })).toHaveAttribute('aria-pressed', 'true')
      await expect(page.getByRole('alert').filter({ hasText: 'range' })).toHaveCount(0)
      // Headline never changes with the chart window.
      await expect(band).toContainText(expected)
      const last = body.data[body.data.length - 1]
      expect(last.total).toMatch(/^3680\.4/)
    }
    expect(lens['6M']).toBeGreaterThan(lens['3M'])
    expect(lens['3Y']).toBeGreaterThanOrEqual(lens['6M'])

    // #528: opening balances are dated today but are the day-0 anchor, so
    // month-ends that hold older rows include the starting balance instead
    // of only the flows (which would read 179.75 / 300.00 here).
    const pointAt = (m: number) => {
      const month = monthsAgo(m).slice(0, 7)
      return series.find((p: { date: string }) => p.date.startsWith(month))?.total
    }
    expect(pointAt(2)).toMatch(/^3680\.4/)
    expect(pointAt(5)).toMatch(/^2800\.55/)
    expect(Number(pointAt(9) ?? '0')).toBe(0)
    await expectNoOverflow(page)

    await page.reload()
    await expect(page.locator('section', { hasText: 'Net worth' }).first()).toContainText(expected)
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function apiPost(cookie: string, path: string, body: unknown) {
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
