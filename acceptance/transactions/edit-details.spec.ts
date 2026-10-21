import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily review (#569): a user fixes a statement row's merchant and adds a
// note from the row menu. The fixed name shows in the list and Top merchants,
// and a merchant-rename rule applied afterwards renames the other matching
// row but keeps the user's name. Money never moves. (Plaid resync keeping the
// name is covered by the Go MergePlaidUpdate tests.) Throwaway user; runs
// under both desktop and mobile projects.

test.describe('edit details journey', () => {
  test('fix a merchant, add a note; rules keep the fix; spending unchanged', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `edit-details-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const dining = cats.data.find((x: { slug: string }) => x.slug === 'food-and-dining') as { id: number }
    const acct = (await api(cookie, '/accounts', {
      name: 'Edit Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (description: string, amount: string) =>
      // Uncategorized, so the rule below may act on them (manual picks win).
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: day, source: 'csv' })
    const coffee = (await tx('QAEDIT SQ *BLUE BTL 0412', '-6.50')).data.id
    const other = (await tx('QAEDIT SQ *BLUE BTL 0415', '-7.25')).data.id
    const spendBefore = await spending(cookie)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    const label = 'QAEDIT SQ *BLUE BTL 0412'
    await expect(page.getByText(label).first()).toBeVisible()

    await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
    const menu = page.getByRole('menu', { name: `Actions for ${label}` })
    await expect(menu.getByRole('menuitem').first()).toHaveText('Edit details')
    await menu.getByRole('menuitem', { name: `Edit details for ${label}` }).click()

    const dlg = page.getByRole('dialog', { name: 'Edit details' })
    await expect(dlg).toBeVisible()
    await expect(dlg).toContainText('Bank description')
    await expect(dlg).toContainText(label)
    await expect(dlg).toContainText('Amount, date and balance don’t change.')
    await expect(dlg).toBeInViewport()
    await dlg.getByLabel('Merchant').fill('Blue Bottle Coffee')
    await dlg.getByLabel('Notes').fill('Coffee with Sam — expense it')
    const save = dlg.getByRole('button', { name: 'Save' })
    await expect(save).toBeInViewport()
    const sb = (await save.boundingBox())!
    expect(sb.height, 'Save target height').toBeGreaterThanOrEqual(40)
    await testInfo.attach(`${testInfo.project.name}-edit-details`, { body: await page.screenshot(), contentType: 'image/png' })
    await save.click()
    await expect(dlg).toBeHidden()

    // The list shows the fixed merchant and a note chip that reopens the editor.
    const row = page.locator('tbody tr').filter({ hasText: label })
    await expect(row.getByText('Blue Bottle Coffee').locator('visible=true').first()).toBeVisible()
    const chip = row.getByRole('button', { name: /^Note: Coffee with Sam/ })
    await expect(chip).toBeVisible()
    const vp = page.viewportSize()!
    if (vp.width < 768) {
      // No hover on a phone: the note is written out under the row.
      await expect(row.getByText('Coffee with Sam — expense it').locator('visible=true')).toBeVisible()
      const box = (await row.boundingBox())!
      expect(box.height, 'row with a note on a phone').toBeLessThanOrEqual(150)
    } else {
      await expect(chip).toHaveAttribute('title', 'Coffee with Sam — expense it')
    }
    await testInfo.attach(`${testInfo.project.name}-row-with-note`, { body: await page.screenshot(), contentType: 'image/png' })
    await expectNoOverflow(page)
    await chip.click()
    await expect(dlg).toBeVisible()
    await expect(dlg.getByLabel('Merchant')).toHaveValue('Blue Bottle Coffee')
    await dlg.getByRole('button', { name: 'Cancel' }).click()
    await expect(dlg).toBeHidden()

    // A rename rule applied to existing rows renames the other match but
    // keeps the user's fix.
    await api(cookie, '/categorization-rules', {
      pattern: 'QAEDIT SQ *BLUE BTL', match_type: 'contains', category_id: dining.id, rename_merchant: 'Blue Bottle', priority: 50,
    })
    await api(cookie, '/categorization-rules/apply', {})
    expect((await get(cookie, `/transactions/${coffee}`)).data.merchant_name).toBe('Blue Bottle Coffee')
    expect((await get(cookie, `/transactions/${other}`)).data.merchant_name).toBe('Blue Bottle')
    const top = (await get(cookie, '/dashboard/top-merchants?limit=20')).data as { merchant: string }[]
    expect(top.map((m) => m.merchant)).toEqual(expect.arrayContaining(['Blue Bottle Coffee', 'Blue Bottle']))

    // Clearing the merchant hands the name back to the bank/rules.
    await page.reload()
    await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
    await page.getByRole('menu').getByRole('menuitem', { name: `Edit details for ${label}` }).click()
    await dlg.getByLabel('Merchant').fill('')
    await dlg.getByRole('button', { name: 'Save' }).click()
    await expect(dlg).toBeHidden()
    await api(cookie, '/categorization-rules/apply', {})
    expect((await get(cookie, `/transactions/${coffee}`)).data.merchant_name).toBe('Blue Bottle')

    expect(await spending(cookie)).toBe(spendBefore)
  })
})

async function spending(cookie: string) {
  const res = await get(cookie, '/dashboard/spend-by-category?period=current_month')
  let total = 0
  for (const c of res.data ?? []) total += Math.abs(Number(c.amount))
  return total.toFixed(2)
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
