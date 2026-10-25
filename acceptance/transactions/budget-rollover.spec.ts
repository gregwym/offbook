import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Rollover budget journey (#508, Monarch BUD-01): a rollover budget created
// with a starting balance carries it in immediately (available = limit +
// start), the card explains where the extra came from, the value round-trips
// through Edit, and turning rollover off drops back to the plain limit.
// Throwaway user; runs under desktop and mobile projects.

test.describe('rollover budget journey', () => {
  test('starting balance carries in, edit round-trips, rollover off', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rollover-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const dining = cats.data.find((x: { slug: string }) => x.slug === 'food-and-dining') as { id: number; name: string }
    if (!dining) throw new Error('category food-and-dining missing')
    const acct = (await api(cookie, '/accounts', {
      name: 'Rollover Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const day1 = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    await api(cookie, '/transactions', {
      account_id: acct, amount: '-150.00', description: 'QA Bistro', transaction_date: day1, source: 'csv', category_id: dining.id,
    })

    await signIn(page, user.email, user.password)
    await page.goto('/budgets')
    await expect(page.getByRole('heading', { name: 'Budgets', exact: true })).toBeVisible()

    await page.getByRole('button', { name: 'New budget' }).click()
    await page.getByLabel('Category').selectOption({ label: dining.name })
    await page.getByLabel('Amount').fill('200.00')
    // The starting balance only appears once rollover is on.
    await expect(page.getByRole('textbox', { name: 'Starting balance' })).toHaveCount(0)
    await page.getByRole('checkbox', { name: /Roll unspent balance/ }).check()
    const start = page.getByRole('textbox', { name: 'Starting balance' })
    await expect(start).toBeVisible()
    await expectNoOverflow(page)
    await start.fill('-5')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Enter a starting balance of 0 or more')).toBeVisible()
    await start.fill('40.00')
    await page.getByRole('button', { name: 'Create', exact: true }).click()

    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: dining.name, exact: true }) }).last()
    // 150 of (200 + 40) = 62.5%, 90 remaining.
    await expect(card).toContainText('monthly · rollover')
    await expect(card).toContainText('240.00')
    await expect(card).toContainText('Includes 40.00 USD rolled over (starting balance plus unspent from earlier periods)')
    await expect(card).toContainText('63%')
    await expect(card).toContainText('90.00 USD remaining')
    await expectNoOverflow(page)

    // Edit shows the saved value without NUMERIC padding.
    await card.getByRole('button', { name: 'Edit' }).click()
    await expect(page.getByRole('textbox', { name: 'Starting balance' })).toHaveValue('40')
    await page.getByRole('checkbox', { name: /Roll unspent balance/ }).uncheck()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(card).not.toContainText('rolled over')
    await expect(card).toContainText('50.00 USD remaining')
    await expect(card).toContainText('75%')

    // Re-enabling restores the kept starting balance; persists on reload.
    await card.getByRole('button', { name: 'Edit' }).click()
    await page.getByRole('checkbox', { name: /Roll unspent balance/ }).check()
    await expect(page.getByRole('textbox', { name: 'Starting balance' })).toHaveValue('40')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(card).toContainText('90.00 USD remaining')
    await page.reload()
    await expect(card).toContainText('Includes 40.00 USD rolled over')
    await expect(card).toContainText('90.00 USD remaining')
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
