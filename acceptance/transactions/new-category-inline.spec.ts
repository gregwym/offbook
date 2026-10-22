import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// New category from the picker (#581): on an uncategorised row the user picks
// "+ New category…", creates "Pet care" in the dialog, and the row is filed
// under it in the same step. The rule form on the same page offers it without
// a reload, Insights spending lists it with the row's amount, and the Budgets
// form offers it. Throwaway user; desktop and mobile projects.

test.describe('new category from a picker', () => {
  test('create a category while categorising a transaction', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `new-category-inline-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = (await api(cookie, 'POST', '/accounts', {
      name: 'Pet Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const label = 'QAPET VET CLINIC 0921'
    await api(cookie, 'POST', '/transactions', {
      account_id: acct, amount: '-45.00', description: label, transaction_date: day, source: 'csv',
    })

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    const row = page.locator('tbody tr').filter({ hasText: label })
    const picker = row.getByRole('combobox', { name: `Category for ${label}` }).locator('visible=true')
    await expect(picker.locator('option', { hasText: '+ New category…' })).toHaveCount(1)
    await picker.selectOption({ label: '+ New category…' })

    const dlg = page.getByRole('dialog', { name: 'New category' })
    await expect(dlg).toBeVisible()
    await expect(dlg).toBeInViewport()
    await expect(dlg.getByLabel('Name')).toBeFocused()
    await expect(dlg.getByRole('link', { name: 'Manage categories' })).toHaveAttribute('href', '/settings#categories')
    // The row didn't jump to a placeholder while the dialog is open.
    await expect(picker.locator('option:checked')).toHaveText('Uncategorized')

    // A taken name is refused inline, in a sentence.
    await dlg.getByLabel('Name').fill('groceries')
    await dlg.getByRole('button', { name: 'Create' }).click()
    await expect(dlg.getByRole('alert')).toContainText('“groceries” already exists. Pick a different name.')

    await dlg.getByLabel('Name').fill('Pet care')
    for (const name of ['Create', 'Cancel']) {
      const box = (await dlg.getByRole('button', { name }).boundingBox())!
      expect(box.height, `${name} target height`).toBeGreaterThanOrEqual(40)
    }
    await expectNoClipping(page, 'new category dialog')
    await testInfo.attach(`${testInfo.project.name}-new-category-dialog`, {
      body: await page.screenshot(), contentType: 'image/png',
    })
    await dlg.getByRole('button', { name: 'Create' }).click()
    await expect(dlg).toBeHidden()

    // The row is filed under it, and focus is back on its picker.
    await expect(picker.locator('option:checked')).toHaveText('Pet care')
    await expect(picker).toBeFocused()
    await testInfo.attach(`${testInfo.project.name}-row-categorised`, {
      body: await page.screenshot(), contentType: 'image/png',
    })
    const tx = (await api(cookie, 'GET', `/transactions?search=${encodeURIComponent('QAPET')}`)).data[0]
    expect(tx.categorization_method).toBe('manual')

    // Same page, no reload: the rule form offers it.
    await row.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
    await page.getByRole('menu').getByRole('menuitem', { name: `Create rule from ${label}` }).click()
    const ruleDlg = page.getByRole('dialog', { name: 'New rule' })
    await expect(ruleDlg.getByLabel('Category', { exact: true }).locator('option', { hasText: 'Pet care' })).toHaveCount(1)
    await ruleDlg.getByRole('button', { name: 'Cancel' }).click()

    // Insights spending lists it with the row's amount.
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    await expect(band.getByRole('listitem').filter({ hasText: 'Pet care' }).first()).toContainText('45.00')

    // The Budgets form offers it.
    await page.goto('/budgets')
    await page.getByRole('button', { name: /New budget/ }).first().click()
    const budgetCat = page.getByRole('dialog', { name: 'New budget' }).getByLabel('Category', { exact: true })
    await expect(budgetCat.locator('option', { hasText: 'Pet care' })).toHaveCount(1)
    await expect(budgetCat.locator('option', { hasText: '+ New category…' })).toHaveCount(1)
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
