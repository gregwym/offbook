import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Delete or merge your own category (#575): "Coffee" (2 transactions, a
// rule, a 50.00 budget) is merged into "Cafes" (1 transaction, a 30.00
// budget) from Settings. The delete panel shows what uses Coffee and refuses
// to go on until a destination is picked. Afterwards Insights shows Cafes
// with all 27.50, the Cafes budget is 80.00 with 27.50 spent, and Coffee is
// gone from every picker. Cafes is then deleted to Uncategorized. Throwaway
// user; runs under both the desktop and the narrow mobile project.

test.describe('delete and merge categories', () => {
  test('merge Coffee into Cafes, then delete Cafes', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `delete-merge-categories-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = (await api(cookie, 'POST', '/accounts', {
      name: 'Merge Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const coffee = (await api(cookie, 'POST', '/categories', { name: 'Coffee' })).data.id as number
    const cafes = (await api(cookie, 'POST', '/categories', { name: 'Cafes' })).data.id as number
    const now = new Date()
    const day = (n: number) =>
      `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-${String(n).padStart(2, '0')}`
    const rows: [string, string, number][] = [
      ['Mergecafe Latte', '-4.50', coffee], ['Mergecafe Beans', '-18.00', coffee], ['Mergecafe Croissant', '-5.00', cafes],
    ]
    for (const [i, [description, amount, category_id]] of rows.entries()) {
      await api(cookie, 'POST', '/transactions', {
        account_id: acct, amount, description, transaction_date: day(i + 1), source: 'csv', category_id,
      })
    }
    await api(cookie, 'POST', '/categorization-rules', { pattern: 'MERGECAFE', match_type: 'contains', category_id: coffee })
    await api(cookie, 'POST', '/budgets', { category_id: coffee, period: 'monthly', amount: '50.00' })
    await api(cookie, 'POST', '/budgets', { category_id: cafes, period: 'monthly', amount: '30.00' })

    await signIn(page, user.email, user.password)
    await page.goto('/settings')
    const section = page.getByRole('region', { name: 'Categories' })
    await section.getByRole('button', { name: 'Delete or merge Coffee' }).click()
    const panel = section.getByRole('form', { name: 'Delete “Coffee”' })
    await expect(panel).toContainText('Used by 2 transactions, 1 rule and 1 budget.')

    // No destination yet: nothing happens but an inline prompt.
    await panel.getByRole('button', { name: 'Delete “Coffee”' }).click()
    await expect(panel.getByRole('alert')).toContainText('Choose where to move what uses this category.')
    await testInfo.attach(`${testInfo.project.name}-delete-needs-destination`, {
      body: await section.screenshot(), contentType: 'image/png',
    })

    await panel.getByLabel('Move everything to').selectOption({ label: 'Cafes' })
    await expect(panel).toContainText('move to “Cafes”, which merges the two')
    await expect(panel).toContainText('A budget for a period “Cafes” already budgets is added to that budget.')
    const merge = panel.getByRole('button', { name: 'Merge into “Cafes”' })
    expect((await merge.boundingBox())!.height, 'Merge target height').toBeGreaterThanOrEqual(40)
    await expectNoClipping(page, 'settings delete category panel')
    await testInfo.attach(`${testInfo.project.name}-merge-confirm`, {
      body: await section.screenshot(), contentType: 'image/png',
    })
    await merge.click()
    await expect(section.getByRole('status')).toContainText('Merged “Coffee” into “Cafes”')
    await expect(section.getByRole('button', { name: 'Delete or merge Coffee' })).toHaveCount(0)
    await expect(section.getByRole('listitem').filter({ hasText: 'Cafes' })).toBeVisible()
    await testInfo.attach(`${testInfo.project.name}-merged`, {
      body: await section.screenshot(), contentType: 'image/png',
    })

    // Insights spending: all three rows under Cafes.
    await page.goto('/insights')
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    await expect(band.getByRole('listitem').filter({ hasText: 'Cafes' }).first()).toContainText('27.50')
    await expect(band.getByRole('listitem').filter({ hasText: 'Coffee' })).toHaveCount(0)

    // Budgets: Coffee's 50.00 folded into Cafes' 30.00.
    await page.goto('/budgets')
    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: 'Cafes', exact: true }) }).last()
    await expect(card).toContainText('27.50')
    await expect(card).toContainText('80.00')
    await expect(page.getByRole('heading', { name: 'Coffee', exact: true })).toHaveCount(0)
    await testInfo.attach(`${testInfo.project.name}-budgets-after-merge`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // The API agrees: the rule now sets Cafes, and Coffee is gone.
    const rules = await api(cookie, 'GET', '/categorization-rules')
    expect(rules.data.map((r: { category_id: number }) => r.category_id)).toEqual([cafes])
    const cats = await api(cookie, 'GET', '/categories')
    expect(cats.data.some((c: { id: number }) => c.id === coffee)).toBe(false)

    // Delete Cafes to Uncategorized.
    await page.goto('/settings')
    await section.getByRole('button', { name: 'Delete or merge Cafes' }).click()
    const panel2 = section.getByRole('form', { name: 'Delete “Cafes”' })
    await expect(panel2).toContainText('Used by 3 transactions, 1 rule and 1 budget.')
    await panel2.getByLabel('Move everything to').selectOption({ label: 'Uncategorized' })
    await expect(panel2).toContainText('Budgets for “Cafes” are removed.')
    await panel2.getByRole('button', { name: 'Delete “Cafes”' }).click()
    await expect(section.getByRole('status')).toContainText('Deleted “Cafes” and moved 3 transactions')
    await expect(section).toContainText("You haven't added any yet")
    await expectNoClipping(page, 'settings categories after delete')
    await testInfo.attach(`${testInfo.project.name}-deleted`, {
      body: await section.screenshot(), contentType: 'image/png',
    })

    const uncategorized = await api(cookie, 'GET', '/transactions?category_id=null&search=Mergecafe')
    expect(uncategorized.total).toBe(3)
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
