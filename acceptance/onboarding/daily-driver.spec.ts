import { expect, test, type Locator, type Page } from '@playwright/test'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1 as ONE journey (#623): a brand-new user goes from an
// empty book to a trusted month using only the UI — add an account, import a
// statement, recategorize and split in the review inbox, clear the inbox,
// read spending on Insights, budget a category, and check net worth. Every
// hand-off is checked against the same exact decimals, so a split counted
// differently by Insights and Budgets, or a net worth that disagrees with
// the account, fails here even when each surface's own spec passes.
// Throwaway user; desktop and narrow mobile projects.

const PAYROLL = 'Daily Payroll'
const SUPERSTORE = 'Daily Superstore'
const FUEL = 'Daily Fuel Stop'
const GROCER = 'Daily Grocer'

test.describe('daily driver journey', () => {
  test('empty book → import → review/categorize/split → spending → budget → net worth', async ({ page }, testInfo) => {
    test.setTimeout(120_000)
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `daily-driver-${testInfo.project.name}` })
    const shot = async (name: string) =>
      testInfo.attach(`${testInfo.project.name}-${name}`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    const now = new Date()
    const day1 = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const statement = [
      'Date,Description,Amount',
      `${day1},${PAYROLL},2500.00`,
      `${day1},${SUPERSTORE},-120.40`,
      `${day1},${FUEL},-45.10`,
      `${day1},${GROCER},-33.30`,
    ].join('\n')
    // Exact expectations. Spending 120.40 + 45.10 + 33.30 = 198.80;
    // Groceries = 80.25 (split part) + 33.30 = 113.55; Shopping 40.15;
    // Gas 45.10. Net worth = 2000.00 opening + 2500.00 - 198.80 = 4301.20.

    await signIn(page, user.email, user.password)

    // 1. Empty Insights → add a manual account with an opening balance.
    await page.goto('/insights')
    await expect(page.getByRole('heading', { name: 'Add your first account' })).toBeVisible()
    await page.getByRole('link', { name: /add your first account/i }).first().click()
    await expect(page).toHaveURL(/\/accounts\/add$/)
    await page.getByRole('button', { name: 'Add manually', exact: true }).click()
    await page.getByLabel('Account name').fill('Daily Checking')
    await page.getByLabel('Institution').fill('Ally Bank')
    await page.getByLabel('Current balance (optional)').fill('2000.00')
    await page.getByLabel('Current balance (optional)').press('Enter')
    await expect(page).toHaveURL(/\/accounts$/)

    // 2. Import the month's statement from the "next step" prompt.
    const confirm = page.getByRole('status').filter({ hasText: 'Daily Checking added.' })
    await confirm.getByRole('button', { name: 'Import transactions' }).click()
    const dialog = page.getByRole('dialog', { name: 'Import transactions' })
    await expect(dialog).toBeVisible()
    await dialog.locator('input[type=file]').setInputFiles({ name: 'month.csv', mimeType: 'text/csv', buffer: Buffer.from(statement) })
    await dialog.getByRole('button', { name: 'Preview' }).click()
    await expect(dialog.getByText('4 new', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Import 4 new' }).click()
    await expect(dialog).toBeHidden()
    await expect(page.getByText('4,301.20').first()).toBeVisible()
    await expectNoClipping(page, 'accounts after import')

    // 3. Review inbox: every imported row is waiting there.
    await page.goto('/transactions')
    await page.getByRole('button', { name: /needs review/i }).first().click()
    for (const d of [PAYROLL, SUPERSTORE, FUEL, GROCER]) {
      await expect(page.getByLabel(`Select ${d}`)).toBeVisible()
    }

    // Categorize three rows with the row picker.
    await categorize(page, FUEL, 'Gas')
    await categorize(page, GROCER, 'Groceries')
    await categorize(page, PAYROLL, 'Income')

    // Split the superstore receipt: groceries plus household shopping.
    await rowAction(page, SUPERSTORE, `Split ${SUPERSTORE}`)
    const split = page.getByRole('dialog', { name: `Split ${SUPERSTORE}` })
    await expect(split).toBeVisible()
    await split.getByLabel('Category for part 1').selectOption({ label: 'Groceries' })
    await split.getByLabel('Amount for part 1').fill('80.25')
    await expect(split.getByLabel('Amount for part 2')).toHaveValue('40.15')
    await split.getByLabel('Category for part 2').selectOption({ label: 'Shopping' })
    await expect(split.getByRole('status')).toHaveText('Fully allocated.')
    await expectNoClipping(page, 'split dialog')
    await shot('split')
    await split.getByRole('button', { name: 'Save split' }).click()
    await expect(split).toBeHidden()
    await expect(page.getByRole('button', { name: `Edit split for ${SUPERSTORE}` }).locator('visible=true')).toBeVisible()

    // Edits don't silently clear the inbox: the rows are still there until
    // the user marks them reviewed.
    for (const d of [PAYROLL, SUPERSTORE, FUEL, GROCER]) await page.getByLabel(`Select ${d}`).check()
    await expectNoClipping(page, 'review inbox with selection')
    await shot('review-selected')
    await page.getByRole('region', { name: 'Bulk actions' }).getByRole('button', { name: /mark reviewed/i }).click()
    await expect(page.getByText(/all caught up/i)).toBeVisible()
    // The inbox empties optimistically; the toast is the server-confirmed
    // "saved" signal, so wait for it before leaving the page.
    await expect(page.getByRole('status').filter({ hasText: '4 transactions marked reviewed' })).toBeVisible()

    // Reload: categories, the split and the reviewed state all persisted.
    await page.reload()
    await expect(page.getByText(/all caught up/i)).toBeVisible()
    // The filter is URL-persisted; turning it off shows the full list.
    const reviewChip = page.getByRole('button', { name: /needs review/i }).first()
    await expect(reviewChip).toHaveAttribute('aria-pressed', 'true')
    await reviewChip.click()
    await expect(page.getByRole('button', { name: `Edit split for ${SUPERSTORE}` }).locator('visible=true')).toBeVisible()
    await expect(picker(page, FUEL).locator('option:checked')).toHaveText('Gas')
    await expect(picker(page, GROCER).locator('option:checked')).toHaveText('Groceries')

    // 4. Insights: the month's totals and category amounts honor the split.
    await page.goto('/insights')
    const totals = page.getByLabel(/^[A-Z][a-z]+ \d{4} totals$/)
    await expect(totals).toContainText('Spending198.80')
    await expect(totals).toContainText('Income2,500.00')
    const spending = page.locator('section', { hasText: 'Spending by category' }).first()
    await expect(categoryRow(spending, 'Groceries')).toContainText('113.55')
    await expect(categoryRow(spending, 'Shopping')).toContainText('40.15')
    await expect(categoryRow(spending, 'Gas')).toContainText('45.10')
    await expectNoClipping(page, 'insights populated')
    await shot('insights')

    // 5. Budget the split category: Budgets counts exactly what Insights did.
    await page.goto('/budgets')
    await page.getByRole('button', { name: 'New budget' }).click()
    await page.getByLabel('Category').selectOption({ label: 'Groceries' })
    await page.getByLabel('Amount').fill('150.00')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: 'Groceries', exact: true }) }).last()
    await expect(card).toContainText('113.55')
    await expect(card).toContainText('150.00')
    await expect(card).toContainText('36.45')
    await expect(card).not.toContainText('over')
    await expectNoClipping(page, 'budgets')
    await shot('budgets')

    // Insights' budget band agrees with the Budgets card.
    await page.goto('/insights')
    const budgetBand = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: /budgets · this period/i }) }).last()
    const budgetRow = budgetBand.getByRole('listitem').filter({ hasText: 'Groceries' })
    await expect(budgetRow).toContainText('113.55')
    await expect(budgetRow).toContainText('150.00')

    // 6. Net worth: opening balance + the month's net flow, exactly, and the
    // trend's latest point is the same figure.
    const trend = page.waitForResponse((r) => r.url().includes('/dashboard/net-worth') && r.url().includes('months='))
    await page.reload()
    const series: { total: string }[] = (await (await trend).json()).data
    expect(series.length).toBeGreaterThan(0)
    expect(series[series.length - 1].total).toMatch(/^4301\.2/)
    const netWorth = page.locator('section', { hasText: 'Net worth' }).first()
    await expect(netWorth).toContainText('4,301.20')
    await expect(netWorth).not.toContainText('partial')
    await expectNoClipping(page, 'insights with budget')
    await shot('net-worth')
  })
})

function picker(page: Page, label: string) {
  return page.getByRole('combobox', { name: `Category for ${label}` }).locator('visible=true')
}

async function categorize(page: Page, label: string, category: string) {
  const p = picker(page, label)
  await p.selectOption({ label: category })
  await expect(p.locator('option:checked')).toHaveText(category)
}

function categoryRow(band: Locator, name: string) {
  return band.getByRole('listitem').filter({ hasText: name }).first()
}

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill(email)
  await page.getByLabel(/password/i).fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/h\/insights|\/accounts/)
}

// A row's secondary actions sit behind "More actions" at every width
// (#554 phone, #563 desktop).
async function rowAction(page: Page, label: string, name: string) {
  await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
  await page.getByRole('menu').getByRole('menuitem', { name, exact: true }).click()
}
