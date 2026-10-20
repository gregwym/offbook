import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// TX-01 tags (#544): a user tags two trip expenses (the second in a different
// case, which reuses the first spelling), then finds them again from a tag
// chip and the Tag filter. Tags are metadata only: spending must not move.
// Throwaway user; runs under both desktop and mobile projects.

test.describe('tags journey', () => {
  test('tag rows, reuse the spelling, filter by tag; spending unchanged', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `tags-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const groceries = cats.data.find((x: { slug: string }) => x.slug === 'groceries') as { id: number }
    const acct = (await api(cookie, '/accounts', {
      name: 'Tags Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (description: string, amount: string) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: day, source: 'csv', category_id: groceries.id })
    await tx('QATAG Hotel Roma', '-210.00')
    await tx('QATAG Trattoria', '-64.50')
    await tx('QATAG Corner grocer', '-25.25')
    const spendBefore = await spending(cookie)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    await expect(page.getByText('QATAG Corner grocer').first()).toBeVisible()

    // First tag: typed and added, with the explainer copy visible.
    const tagsFor = (label: string) => page.getByRole('button', { name: `Tags for ${label}` })
    await expect(tagsFor('QATAG Hotel Roma')).toBeInViewport()
    await tagsFor('QATAG Hotel Roma').click()
    let dlg = page.getByRole('dialog', { name: 'Tags for QATAG Hotel Roma' })
    await expect(dlg).toContainText('They don’t change spending, budgets or balances.')
    await expect(dlg).toContainText('No tags yet.')
    await dlg.getByLabel('New tag').fill('Rome trip')
    await dlg.getByRole('button', { name: 'Add', exact: true }).click()
    await expect(dlg.getByRole('list', { name: 'Current tags' })).toContainText('Rome trip')
    await testInfo.attach(`${testInfo.project.name}-tag-editor`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dlg.getByRole('button', { name: 'Save tags' }).click()
    await expect(dlg).toBeHidden()
    await expect(page.getByRole('button', { name: 'Show transactions tagged Rome trip' })).toHaveCount(1)

    // Second tag in another case: typed then saved directly; the server
    // keeps the user's existing spelling, so there is still one tag.
    await tagsFor('QATAG Trattoria').click()
    dlg = page.getByRole('dialog', { name: 'Tags for QATAG Trattoria' })
    await expect(dlg.getByRole('button', { name: 'Add tag Rome trip' })).toBeVisible()
    await dlg.getByLabel('New tag').fill('rome TRIP')
    await dlg.getByRole('button', { name: 'Save tags' }).click()
    await expect(dlg).toBeHidden()
    await expect(page.getByRole('button', { name: 'Show transactions tagged Rome trip' })).toHaveCount(2)
    const tags = await get(cookie, '/transactions/tags')
    expect(tags.data).toEqual([{ name: 'Rome trip', count: 2 }])

    // Tag chip -> filtered list with the Tag filter set.
    await page.getByRole('button', { name: 'Show transactions tagged Rome trip' }).first().click()
    await expect(page.getByText('QATAG Corner grocer').locator('visible=true')).toHaveCount(0)
    await expect(page.getByText('QATAG Hotel Roma').locator('visible=true').first()).toBeVisible()
    await expect(page.getByText('QATAG Trattoria').locator('visible=true').first()).toBeVisible()
    const tagFilter = page.locator('label', { hasText: /^Tag/ }).locator('select')
    await expect(tagFilter).toHaveValue('Rome trip')
    await expect(tagFilter.locator('option', { hasText: 'Rome trip (2)' })).toHaveCount(1)
    await expect(tagFilter).toBeInViewport()
    await testInfo.attach(`${testInfo.project.name}-tag-filtered`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await expectNoOverflow(page)

    // Clearing the filter brings everything back; spending never moved.
    await tagFilter.selectOption('')
    await expect(page.getByText('QATAG Corner grocer').locator('visible=true').first()).toBeVisible()
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
