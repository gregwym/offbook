import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Wrong transfer guess journey (#510, Monarch TX-04): a coincidental
// opposite-amount pair (a card purchase and an unrelated deposit) is proposed
// as a transfer and left out of spending and the budget. The user says
// "Not a transfer" and both Insights spending and the budget actual rise by
// exactly that amount and stay that way after a reload. Throwaway user; runs
// under desktop and mobile projects.

test.describe('reject a wrong transfer match', () => {
  test('not a transfer counts both rows again', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `xfer-dismiss-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const dining = cats.data.find((x: { slug: string }) => x.slug === 'food-and-dining') as { id: number; name: string }
    if (!dining) throw new Error('category food-and-dining missing')
    const mk = (name: string, account_type: string) =>
      api(cookie, '/accounts', { name, institution_slug: 'manual', account_type, currency: 'USD' })
    const card = (await mk('QA Card', 'credit_card')).data.id
    const checking = (await mk('QA Checking', 'checking')).data.id
    const now = new Date()
    const day1 = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (account_id: number, amount: string, description: string, category_id?: number) =>
      api(cookie, '/transactions', { account_id, amount, description, transaction_date: day1, source: 'csv', category_id })
    await tx(checking, '-100.00', 'QA Bistro', dining.id)
    await tx(card, '-50.00', 'QA Corner Cafe', dining.id)
    await tx(checking, '50.00', 'QA Venmo from Sam')
    await api(cookie, '/budgets', { category_id: dining.id, period: 'monthly', amount: '200.00' })

    await signIn(page, user.email, user.password)

    // Before deciding: the 50.00 pair is a proposed transfer, so only the
    // 100.00 bistro spend counts.
    await expectSpending(page, '100.00')
    await expectBudget(page, dining.name, '100.00 USD remaining')

    await page.goto('/transactions')
    const banner = page.getByText(/1 possible transfer found between your own accounts/i)
    await expect(banner).toBeVisible()
    await expect(banner).toContainText("It isn't counted in income or spending until you")
    await expectNoOverflow(page)
    await snap(page, testInfo, 'banner')

    await page.getByRole('button', { name: 'review it' }).click()
    const dialog = page.getByRole('dialog', { name: 'Possible transfers' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('QA Card')
    await expect(dialog).toContainText('QA Checking')
    const notTransfer = dialog.getByRole('button', { name: /^Not a transfer/ })
    const confirm = dialog.getByRole('button', { name: /^Confirm match/ })
    await expect(notTransfer).toBeVisible()
    await expect(confirm).toBeVisible()
    await expectNoOverflow(page)
    await snap(page, testInfo, 'modal')

    await notTransfer.click()
    await expect(dialog.getByText('Both transactions now count in income and spending.')).toBeVisible()
    await expect(dialog.getByRole('button', { name: /^Confirm match/ })).toHaveCount(0)
    await snap(page, testInfo, 'dismissed')
    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(banner).toHaveCount(0)

    // After: spending and the dining budget both rise by exactly 50.00.
    await expectSpending(page, '150.00')
    await expectBudget(page, dining.name, '50.00 USD remaining')
    await snap(page, testInfo, 'budget-after')

    // The decision is durable: no proposal comes back after a reload.
    await page.goto('/transactions')
    await expect(page.getByText('QA Venmo from Sam').first()).toBeVisible()
    await expect(page.getByText(/possible transfer/i)).toHaveCount(0)
    await page.reload()
    await expect(page.getByText('QA Venmo from Sam').first()).toBeVisible()
    await expect(page.getByText(/possible transfer/i)).toHaveCount(0)
    await expectSpending(page, '150.00')
  })
})

async function expectSpending(page: Page, amount: string) {
  await page.goto('/insights')
  await expect(page.getByText(/spending:/i).first()).toContainText(amount)
  await expectNoOverflow(page)
}

async function expectBudget(page: Page, category: string, remaining: string) {
  await page.goto('/budgets')
  const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: category, exact: true }) }).last()
  await expect(card).toContainText(remaining)
  await expectNoOverflow(page)
}

async function snap(page: Page, testInfo: TestInfo, name: string) {
  await testInfo.attach(name, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
}

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
