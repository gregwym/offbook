import { expect, test, type Locator, type Page } from '@playwright/test'
import { createThrowawayUser } from '../fixtures/state.mjs'

// First-run journey (ONB-01, Goal #423 gate 1, #520): a brand-new user with no
// data goes from the empty Insights page to their first imported transactions
// and spending using only the UI — add a manual account, follow the
// "import transactions" next step, import a CSV statement, see it on
// Transactions and Insights. Throwaway user; desktop and narrow mobile.

test.describe('first run', () => {
  test('empty Insights → manual account → import statement → spending', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `first-run-${testInfo.project.name}` })

    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    const statement = [
      'Date,Description,Amount',
      `${day(1)},First Payroll,1200.00`,
      `${day(1)},First Coffee,-4.50`,
      `${day(2)},First Grocer,-52.10`,
    ].join('\n')

    await signIn(page, user.email, user.password)

    // Empty Insights: one clear call to action, no error.
    await page.goto('/insights')
    await expect(page.getByRole('heading', { name: 'Add your first account' })).toBeVisible()
    await expect(page.getByRole('button', { name: /refresh prices/i })).toHaveCount(0)
    await expectNoOverflow(page)
    const cta = page.getByRole('link', { name: /add your first account/i }).first()
    await expectInViewport(page, cta)
    await cta.click()

    // Two tiles; pick manual and fill a plain-language form.
    await expect(page).toHaveURL(/\/accounts\/add$/)
    await page.getByRole('button', { name: 'Add manually', exact: true }).click()
    await expect(page.getByText(/slug/i)).toHaveCount(0)
    await page.getByLabel('Account name').fill('First Checking')
    await page.getByLabel('Institution').fill('Ally Bank')
    await expect(page.getByRole('option', { name: 'Credit card' })).toHaveCount(1)
    await page.getByLabel('Current balance (optional)').fill('1000')
    await expectNoOverflow(page)
    await page.getByLabel('Current balance (optional)').press('Enter')

    // Accounts confirms it and offers the next step.
    await expect(page).toHaveURL(/\/accounts$/)
    const confirm = page.getByRole('status').filter({ hasText: 'First Checking added.' })
    await expect(confirm).toBeVisible()
    const next = confirm.getByRole('button', { name: 'Import transactions' })
    await expect(next).toBeEnabled()
    await expectInViewport(page, next)
    await expectNoOverflow(page)
    await next.click()

    // The import dialog is preset to the new account.
    const dialog = page.getByRole('dialog', { name: 'Import transactions' })
    await expect(dialog).toBeVisible()
    await expect(dialog.locator('select').first().locator('option:checked')).toHaveText(/First Checking/)
    await dialog.locator('input[type=file]').setInputFiles({ name: 'first.csv', mimeType: 'text/csv', buffer: Buffer.from(statement) })
    await dialog.getByRole('button', { name: 'Preview' }).click()
    await expect(dialog.getByText('3 new', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Import 3 new' }).click()
    await expect(dialog).toBeHidden()
    await expect(confirm).toHaveCount(0)
    await expect(page.getByText('2,143.40').first()).toBeVisible()

    // Transactions show the imported rows exactly once.
    await page.goto('/transactions')
    for (const d of ['First Payroll', 'First Coffee', 'First Grocer']) await expect(page.getByText(d)).toHaveCount(1)
    await expectNoOverflow(page)

    // Insights is populated: the empty state is gone and spending counts the two debits.
    await page.goto('/insights')
    await expect(page.getByRole('heading', { name: 'Add your first account' })).toHaveCount(0)
    await expect(page.getByText(/spending:/i).first()).toContainText('56.60')

    // #524: allocation names the class in plain language and the donut is
    // fully drawn on first paint (no entry animation leaving it blank).
    const allocation = page.locator('section', { has: page.getByRole('heading', { name: /asset allocation/i }) })
    await allocation.scrollIntoViewIfNeeded()
    await expect(allocation.getByText('Cash', { exact: true })).toBeVisible()
    await expect(allocation).toContainText('100.0%')
    await expect(allocation).not.toContainText(/fiat/i)
    const sector = allocation.locator('.recharts-pie-sector path').first()
    await expect(sector).toBeVisible()
    const box = await sector.boundingBox()
    expect(box!.width, 'donut drawn at full size immediately').toBeGreaterThan(150)
    await expectNoOverflow(page)
  })
})

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

// A key control must be fully on screen horizontally — overflow inside a
// scrolling wrapper passes expectNoOverflow but still hides the control.
async function expectInViewport(page: Page, el: Locator) {
  await el.scrollIntoViewIfNeeded()
  const box = await el.boundingBox()
  const width = page.viewportSize()!.width
  expect(box, 'control is rendered').not.toBeNull()
  expect(box!.x, 'control starts on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'control ends on screen').toBeLessThanOrEqual(width)
}
