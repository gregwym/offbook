import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Custom categories (#573): in Settings a user adds "Coffee" under Food &
// Dining, sees a duplicate name refused inline, files two transactions under
// it from the Transactions bulk bar, finds it in Insights spending, then
// renames it and sees the new name everywhere. Throwaway user; runs under
// both the desktop and the narrow mobile project.

test.describe('custom categories', () => {
  test('add, use and rename your own category', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `custom-categories-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = (await api(cookie, 'POST', '/accounts', {
      name: 'Cat Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    for (const [i, [d, amt]] of [['Catcafe Latte', '-4.50'], ['Catcafe Beans', '-18.00']].entries()) {
      await api(cookie, 'POST', '/transactions', {
        account_id: acct, amount: amt, description: d, transaction_date: day(i + 1), source: 'csv',
      })
    }

    await signIn(page, user.email, user.password)
    await page.goto('/settings')
    const section = page.getByRole('region', { name: 'Categories' })
    await expect(section).toContainText("You haven't added any yet")
    await section.getByLabel('New category').fill('Coffee')
    await section.getByLabel('Group under (optional)').selectOption({ label: 'Food & Dining' })
    const add = section.getByRole('button', { name: /Add category/ })
    expect((await add.boundingBox())!.height, 'Add target height').toBeGreaterThanOrEqual(40)
    await add.click()
    await expect(section.getByRole('status')).toContainText('Added “Coffee”')
    await expect(section.getByRole('listitem').filter({ hasText: 'Coffee' })).toContainText('in Food & Dining')
    await expectNoClipping(page, 'settings categories')
    await testInfo.attach(`${testInfo.project.name}-category-added`, {
      body: await section.screenshot(), contentType: 'image/png',
    })

    // A name that already exists (any case, built-in included) is refused inline.
    await section.getByLabel('New category').fill('groceries')
    await add.click()
    await expect(section.getByRole('alert')).toContainText('“groceries” already exists. Pick a different name.')
    await expect(section.getByRole('status')).toHaveCount(0)
    await expect(section.getByLabel('New category')).toHaveValue('groceries')
    await testInfo.attach(`${testInfo.project.name}-category-duplicate`, {
      body: await section.screenshot(), contentType: 'image/png',
    })
    await section.getByLabel('New category').fill('')

    // The new category is in the Transactions picker straight away.
    await page.goto('/transactions')
    await page.getByLabel('Search').fill('Catcafe')
    await expect(page.getByText('Catcafe Latte').filter({ visible: true }).first()).toBeVisible()
    await page.getByLabel('Select all transactions on this page').check()
    const bar = page.getByRole('region', { name: 'Bulk actions' })
    await bar.getByLabel('Set category for selected').selectOption({ label: 'Coffee' })
    await expect(page.getByRole('status').filter({ hasText: 'moved to Coffee' })).toContainText('2 transactions moved to Coffee.')

    // Insights spending groups them under it.
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    await expect(band.getByRole('listitem').filter({ hasText: 'Coffee' }).first()).toContainText('22.50')

    // Rename keeps the category and its transactions.
    await page.goto('/settings')
    await section.getByRole('button', { name: 'Rename Coffee' }).click()
    await section.getByLabel('New name for Coffee').fill('Coffee shops')
    await section.getByRole('button', { name: 'Save' }).click()
    await expect(section.getByRole('status')).toContainText('Renamed to “Coffee shops”')
    await expect(section.getByRole('listitem').filter({ hasText: 'Coffee shops' })).toBeVisible()
    await expectNoClipping(page, 'settings categories after rename')
    await testInfo.attach(`${testInfo.project.name}-category-renamed`, {
      body: await section.screenshot(), contentType: 'image/png',
    })
    await page.goto('/insights')
    await expect(band.getByRole('listitem').filter({ hasText: 'Coffee shops' }).first()).toContainText('22.50')
    await testInfo.attach(`${testInfo.project.name}-insights-renamed`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function api(cookie: string, method: string, path: string, body?: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method, headers: { 'content-type': 'application/json', cookie }, body: body === undefined ? undefined : JSON.stringify(body),
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
