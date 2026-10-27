import { expect, test, type Locator, type Page } from '@playwright/test'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Cold-start empty states (#625, Goal #423 gate 1): a brand-new user visits
// every personal page before adding anything. Each page explains itself with
// no error banner and no horizontal overflow, and Budgets / Savings goals
// offer their first create action in place — which works end to end.
// Throwaway user; desktop and narrow mobile.

test.describe('empty states', () => {
  test('a new user sees one clear next step on every personal page', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `empty-${testInfo.project.name}` })
    await signIn(page, user.email, user.password)

    const shot = async (name: string) =>
      testInfo.attach(`${testInfo.project.name}-${name}`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    await page.goto('/accounts')
    await expect(page.getByText('No accounts yet.')).toBeVisible()
    await expectInViewport(page, page.getByRole('link', { name: /add your first account/i }).first())
    await expectHealthy(page)
    await shot('accounts')

    await page.goto('/transactions')
    await expect(page.getByText('No transactions yet.')).toBeVisible()
    await expectInViewport(page, page.getByRole('link', { name: 'Add your first account' }))
    await expectHealthy(page)
    await shot('transactions')

    await page.goto('/rules')
    await expect(page.getByText(/No rules yet/)).toBeVisible()
    await expectHealthy(page)
    await shot('rules')

    // Budgets: create the first one from the empty state itself.
    await page.goto('/budgets')
    await expect(page.getByText(/No budgets yet/)).toBeVisible()
    const firstBudget = page.getByRole('button', { name: 'Create your first budget' })
    await expectInViewport(page, firstBudget)
    await expectHealthy(page)
    await shot('budgets-empty')
    await firstBudget.click()
    const budgetDialog = page.getByRole('dialog', { name: 'New budget' })
    await expect(budgetDialog).toBeVisible()
    await budgetDialog.getByLabel('Category').selectOption({ label: 'Groceries' })
    await budgetDialog.getByLabel('Amount').fill('300.00')
    await budgetDialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(budgetDialog).toBeHidden()
    await expect(page.getByText(/No budgets yet/)).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Groceries', exact: true })).toBeVisible()
    await expectHealthy(page)
    await shot('budgets-created')

    // Savings goals: same, plain-language header, button doesn't wrap.
    await page.goto('/savings-goals')
    await expect(page.getByText(/No goals yet/)).toBeVisible()
    await expect(page.getByText(/atomic/i)).toHaveCount(0)
    const header = page.getByRole('button', { name: 'New goal' })
    const hb = await header.boundingBox()
    expect(hb!.height, 'header button stays on one line').toBeLessThan(48)
    const firstGoal = page.getByRole('button', { name: 'Create your first goal' })
    await expectInViewport(page, firstGoal)
    await expectHealthy(page)
    await shot('goals-empty')
    await firstGoal.click()
    const goalDialog = page.getByRole('dialog', { name: 'New goal' })
    await expect(goalDialog).toBeVisible()
    await goalDialog.getByLabel('Name').fill('Emergency fund')
    await goalDialog.getByLabel('Target amount').fill('5000')
    await goalDialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(goalDialog).toBeHidden()
    await expect(page.getByText(/No goals yet/)).toHaveCount(0)
    await expect(page.getByText('Emergency fund').first()).toBeVisible()
    await expectHealthy(page)
    await shot('goals-created')
  })
})

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill(email)
  await page.getByLabel(/password/i).fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/h\/insights|\/accounts/)
}

// No error banner (the shared `border-red-200 bg-red-50` markup) and no
// horizontal page scroll.
async function expectHealthy(page: Page) {
  await expect(page.getByText('Loading…')).toHaveCount(0)
  await expect(page.locator('.border-red-200.bg-red-50')).toHaveCount(0)
  const o = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
  expect(o, 'no horizontal overflow').toBe(false)
}

async function expectInViewport(page: Page, el: Locator) {
  await el.scrollIntoViewIfNeeded()
  const box = await el.boundingBox()
  const width = page.viewportSize()!.width
  expect(box, 'control is rendered').not.toBeNull()
  expect(box!.x, 'control starts on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'control ends on screen').toBeLessThanOrEqual(width)
}
