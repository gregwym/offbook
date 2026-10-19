import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1 journey: monthly budget created through the UI reflects
// real category spending (refunds net, unconfirmed transfers excluded),
// flags an overspent envelope, and survives reload. Throwaway user; runs
// under both desktop and mobile projects.

test.describe('budgets journey', () => {
  test('create budgets, see actuals, over-budget flag, persists', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `budgets-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const cat = (slug: string) => {
      const c = cats.data.find((x: { slug: string }) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c as { id: number; name: string }
    }
    const mk = (name: string) =>
      api(cookie, '/accounts', { name, institution_slug: 'manual', account_type: 'checking', currency: 'USD' })
    const a = (await mk('Budget Checking')).data.id
    const b = (await mk('Budget Savings')).data.id

    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    const tx = (account_id: number, amount: string, description: string, d: number, category_id?: number) =>
      api(cookie, '/transactions', {
        account_id, amount, description, transaction_date: day(d), source: 'csv', category_id,
      })
    const groceries = cat('groceries')
    const gas = cat('gas')
    await tx(a, '-150.00', 'QA Grocer', 1, groceries.id)
    await tx(a, '30.00', 'QA Grocer refund', 1, groceries.id)
    await tx(a, '-60.00', 'QA Gas station', 1, gas.id)
    await tx(a, '-500.00', 'QA Move out', 1)
    await tx(b, '500.00', 'QA Move in', 1)

    await signIn(page, user.email, user.password)
    await page.goto('/budgets')
    await expect(page.getByRole('heading', { name: 'Budgets', exact: true })).toBeVisible()

    const create = async (name: string, amount: string) => {
      await page.getByRole('button', { name: 'New budget' }).click()
      await page.getByLabel('Category').selectOption({ label: name })
      await page.getByLabel('Amount').fill(amount)
      await page.getByRole('button', { name: 'Create', exact: true }).click()
    }

    // Validation: submitting with no category keeps the dialog open with a message.
    await page.getByRole('button', { name: 'New budget' }).click()
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Pick a category.')).toBeVisible()
    await page.getByRole('button', { name: 'Cancel' }).click()

    await create(groceries.name, '200.00')
    await create(gas.name, '50.00')

    const card = (name: string) =>
      page.locator('div.rounded-lg', { has: page.getByRole('heading', { name, exact: true }) }).last()
    // Groceries: 150 - 30 refund = 120 of 200, 60%, 80 remaining; transfer not counted.
    const g = card(groceries.name)
    await expect(g).toContainText('120.00')
    await expect(g).toContainText('200.00')
    await expect(g).toContainText('60%')
    await expect(g).toContainText('80.00')
    await expect(g).not.toContainText('over')
    // Gas: 60 of 50 -> over budget.
    const p = card(gas.name)
    await expect(p).toContainText('60.00')
    await expect(p).toContainText('120% over')
    await expect(p).toContainText('10.00 USD over budget')
    await expect(p).not.toContainText('-10.00')

    await page.reload()
    await expect(card(groceries.name)).toContainText('120.00')
    await expect(card(gas.name)).toContainText('120% over')
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
