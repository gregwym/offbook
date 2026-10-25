import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for bulk edit (#567): filter to one merchant's rows, select them,
// set one category, undo, then hide and unhide them, with the server state
// checked after each step. Uses a throwaway user. Runs under both the desktop
// and the narrow mobile project.

const shop = ['Corner Shop 1', 'Corner Shop 2', 'Corner Shop 3']

test.describe('transaction bulk edit', () => {
  test('set category, undo, hide and unhide a filtered selection', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `bulk-edit-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await api(cookie, 'POST', '/accounts', {
      name: 'Bulk Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    for (const [i, d] of [...shop, 'Salary Deposit'].entries()) {
      await api(cookie, 'POST', '/transactions', {
        account_id: acct.data.id, amount: i === 3 ? '2500.00' : `-${10 + i}.25`, description: d,
        transaction_date: `2026-09-1${i + 1}`, source: 'csv',
      })
    }
    const categories = await api(cookie, 'GET', '/categories')
    const groceries = categories.data.find((c: { slug: string }) => c.slug === 'groceries')

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    await page.getByLabel('Search').fill('Corner Shop')
    await expect(page.getByText('Salary Deposit')).toHaveCount(0)
    for (const d of shop) await expect(page.getByText(d).locator('visible=true').first()).toBeVisible()

    // Select the filtered page and set one category.
    await page.getByLabel('Select all transactions on this page').check()
    const bar = page.getByRole('region', { name: 'Bulk actions' })
    await expect(bar).toContainText('3 selected')
    await expectNoClipping(page, 'bulk bar')
    await page.screenshot({ path: testInfo.outputPath('bulk-bar.png') })
    await bar.getByLabel('Set category for selected').selectOption({ label: 'Groceries' })
    const toast = page.getByRole('status').filter({ hasText: 'moved to Groceries' })
    await expect(toast).toContainText('3 transactions moved to Groceries.')
    await expect(bar).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('bulk-categorized.png') })
    let rows = await listShop(cookie)
    expect(rows.map((t) => t.category_id)).toEqual([groceries.id, groceries.id, groceries.id])
    expect(rows.every((t) => t.categorization_method === 'manual')).toBe(true)
    expect(rows.map((t) => Number(t.amount))).toEqual([-10.25, -11.25, -12.25])

    // Undo restores them to uncategorized.
    await toast.getByRole('button', { name: 'Undo' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Undone' })).toContainText('Undone: 3 transactions restored.')
    rows = await listShop(cookie)
    expect(rows.map((t) => t.category_id ?? null)).toEqual([null, null, null])

    // Hide two of them, then unhide.
    await page.getByLabel('Select Corner Shop 1').check()
    await page.getByLabel('Select Corner Shop 2').check()
    await bar.getByRole('button', { name: /^hide$/i }).click()
    await expect(page.getByRole('status').filter({ hasText: 'hidden' })).toContainText('2 transactions hidden from spending and budgets.')
    rows = await listShop(cookie)
    expect(rows.map((t) => t.hidden_at != null)).toEqual([true, true, false])
    await expect(page.locator('span:visible', { hasText: /^hidden$/ })).toHaveCount(2)
    await page.screenshot({ path: testInfo.outputPath('bulk-hidden.png') })

    await page.getByLabel('Select Corner Shop 1').check()
    await page.getByLabel('Select Corner Shop 2').check()
    await expect(bar.getByRole('button', { name: /^hide$/i })).toHaveCount(0)
    await bar.getByRole('button', { name: /unhide/i }).click()
    await expect(page.getByRole('status').filter({ hasText: 'back in' })).toContainText('2 transactions back in spending and budgets.')
    rows = await listShop(cookie)
    expect(rows.every((t) => t.hidden_at == null)).toBe(true)
    await expectNoClipping(page, 'after bulk edit')
  })
})

type Row = { description: string; category_id?: number | null; categorization_method?: string | null; hidden_at?: string | null; amount: string }

async function listShop(cookie: string): Promise<Row[]> {
  const res = await api(cookie, 'GET', '/transactions?search=Corner%20Shop&limit=50')
  return (res.data as Row[]).sort((a, b) => a.description.localeCompare(b.description))
}

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
