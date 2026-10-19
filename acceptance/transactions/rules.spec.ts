import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1 journey (TX-01): a user drafts a categorization rule,
// sees its live impact before saving, saves it, previews the bulk re-apply
// (nothing changes), applies it, and manual picks survive. Throwaway user;
// runs under both desktop and mobile projects.

test.describe('categorization rules journey', () => {
  test('rule impact preview, dry-run, apply, manual picks win', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const groceries = cats.data.find((x: { slug: string }) => x.slug === 'groceries') as { id: number; name: string }
    const gas = cats.data.find((x: { slug: string }) => x.slug === 'gas') as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Rules Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = (description: string, amount: string, category_id?: number) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv', category_id })
    await tx('QARULE MART #1', '-10.00')
    await tx('QARULE MART #2', '-20.00')
    const manual = (await tx('QARULE MART MANUAL', '-30.00', gas.id)).data
    await tx('Unrelated coffee', '-5.00')

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await expect(page.getByRole('heading', { name: 'Categorization rules' })).toBeVisible()

    // Primary actions must be reachable at any width (not clipped off-screen).
    await expectInViewport(page, page.getByRole('button', { name: 'New rule' }))
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await expect(dialog).toContainText('Fill in pattern and category')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('QARULE MART')
    await dialog.getByLabel('Category').selectOption({ label: groceries.name })
    // Live impact: 3 match, 2 would change, the manual pick is protected.
    await expect(dialog).toContainText('Would categorize')
    await expect(dialog).toContainText('2 would change')
    await expect(dialog).toContainText('1 manual picks are never overridden')
    await expect(dialog).toContainText('Nothing changes until you save')
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('QARULE MART', { exact: true }).first()).toBeVisible()
    await expectInViewport(page, page.getByRole('button', { name: 'Edit' }).first())
    await expectInViewport(page, page.getByRole('button', { name: 'Delete' }).first())

    // Dry run reports without changing anything.
    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/would be re-categorized/)).toBeVisible()
    await expect(page.getByText(/Nothing has been changed/)).toBeVisible()
    let rows = await get(cookie, `/transactions?search=QARULE&limit=50`)
    expect(rows.data.filter((t: { category_id: number | null }) => t.category_id === groceries.id)).toHaveLength(0)

    // Apply from the preview banner.
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(async () => {
      rows = await get(cookie, `/transactions?search=QARULE&limit=50`)
      return rows.data.filter((t: { category_id: number | null }) => t.category_id === groceries.id).length
    }).toBe(2)
    const still = rows.data.find((t: { id: number }) => t.id === manual.id)
    expect(still.category_id).toBe(gas.id)

    await page.reload()
    await expect(page.getByText('QARULE MART', { exact: true }).first()).toBeVisible()
    await expectNoOverflow(page)
  })
})

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

async function expectInViewport(page: Page, loc: ReturnType<Page['getByRole']>) {
  const box = await loc.boundingBox()
  const vw = page.viewportSize()!.width
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'left edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'right edge on screen').toBeLessThanOrEqual(vw)
}
