import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Recurring charges you can trust (#653). A statement CSV holds three
// subscriptions: one whose price went up halfway through, one still
// running, and a gym that stopped six months ago. Insights lists the two
// live ones at their current price with a monthly total, and the gym under
// "No longer charging" with no cost. Throwaway user; runs under both the
// desktop and the narrow mobile project.

test.describe('recurring charges', () => {
  test('a price change keeps the subscription, a stopped one is set apart', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `recurring-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await apiPost(cookie, '/accounts', {
      name: 'Subs Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const ago = (days: number) => new Date(Date.now() - days * 86_400_000).toISOString().slice(0, 10)
    const csv = [
      'Date,Description,Amount',
      ...[150, 120, 90].map((d) => `${ago(d)},STREAMFLIX,-9.99`),
      ...[60, 30, 0].map((d) => `${ago(d)},STREAMFLIX,-15.49`),
      ...[62, 31, 1].map((d) => `${ago(d)},TUNEBOX MUSIC,-10.99`),
      ...[240, 210, 180].map((d) => `${ago(d)},IRONWORKS GYM,-40.00`),
      `${ago(2)},CORNER GROCER,-54.20`,
    ].join('\n')

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')
    await page.getByRole('button', { name: 'Import transactions' }).first().click()
    await page.locator('select').first().selectOption({ label: 'Subs Checking' })
    await page.locator('input[type=file]').setInputFiles({ name: 'subs.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
    await page.getByRole('button', { name: 'Preview' }).click()
    await page.getByRole('button', { name: 'Import 13 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()

    // The API is the exact record: current-run figures and status.
    type Item = { merchant: string, cadence: string, occurrences: number, last_amount: string, status: string }
    const items = (await apiGet(cookie, '/dashboard/recurring')).data as Item[]
    const find = (re: RegExp) => items.find((i) => re.test(i.merchant))
    expect(find(/streamflix/i)).toMatchObject({ cadence: 'monthly', occurrences: 3, last_amount: '15.49', status: 'active' })
    expect(find(/tunebox/i)).toMatchObject({ cadence: 'monthly', occurrences: 3, last_amount: '10.99', status: 'active' })
    expect(find(/ironworks/i)).toMatchObject({ cadence: 'monthly', status: 'lapsed' })
    expect(find(/grocer/i)).toBeUndefined()

    await page.goto('/insights')
    const heading = page.getByRole('heading', { name: 'Recurring charges' })
    await heading.scrollIntoViewIfNeeded()
    await expect(page.getByTestId('recurring-total')).toContainText('/mo across 2 charges')

    const active = page.getByRole('list', { name: 'Active recurring charges' })
    await expect(active.getByRole('listitem')).toHaveCount(2)
    await expect(active.getByRole('listitem').filter({ hasText: /streamflix/i })).toContainText('15.49')
    await expect(active.getByRole('listitem').filter({ hasText: /tunebox/i })).toContainText('10.99')
    await expect(active).not.toContainText(/ironworks/i)

    const stopped = page.getByRole('list', { name: 'Recurring charges that stopped' })
    await expect(page.getByRole('heading', { name: 'No longer charging' })).toBeVisible()
    const gym = stopped.getByRole('listitem').filter({ hasText: /ironworks/i })
    await expect(gym).toContainText('40.00')
    await expect(gym).toContainText('last on')
    await expect(gym).not.toContainText('/mo')
    // Friendly dates, never raw ISO.
    await expect(page.getByRole('heading', { name: 'Recurring charges' }).locator('xpath=ancestor::div[2]')).not.toContainText(/\d{4}-\d{2}-\d{2}/)

    await stopped.scrollIntoViewIfNeeded()
    await testInfo.attach(`${testInfo.project.name}-recurring`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await testInfo.attach(`${testInfo.project.name}-recurring-viewport`, { body: await page.screenshot(), contentType: 'image/png' })
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

async function apiGet(cookie: string, path: string) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, { headers: { cookie } })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
  return res.json()
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
