import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser, withQAClient } from '../fixtures/state.mjs'

// Journey for household budget names (#615): a shared budget created by one
// member must read by its category name for every member on /h/insights and
// /h/budgets, never "Category #N". Throwaway household; desktop and mobile.

test.describe('household budget names', () => {
  test('names a shared budget by its category', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const suite = `hhbudname-${testInfo.project.name}`
    const householdID = await withQAClient(async (client) => {
      const r = await client.query(
        "INSERT INTO households (name, grace_period_days, created_at, updated_at) VALUES ('Budget Names Household', 30, NOW(), NOW()) RETURNING id",
      )
      return Number(r.rows[0].id)
    })
    const owner = await createThrowawayUser({ suite: `${suite}-o`, householdID, role: 'owner' })
    const member = await createThrowawayUser({ suite: `${suite}-m`, householdID, role: 'contributor' })
    const memberCookie = await apiSignin(member.email, member.password)

    // Shared budgets track system categories only.
    const cats: { id: number; name: string; user_id?: number }[] = await apiGet(memberCookie, '/categories')
    const cat = cats.find((c) => c.user_id == null && c.name === 'Groceries') ?? cats.find((c) => c.user_id == null)!
    const catName = cat.name
    await apiPost(memberCookie, `/households/${householdID}/shared-budgets`, {
      category_id: cat.id,
      period: 'monthly',
      amount: '120',
      is_active: true,
    })

    await signIn(page, owner.email, owner.password)
    await page.goto('/h/insights')
    await expect(page.getByText(/budgets · this period/i)).toBeVisible()
    await expect(page.getByText(catName, { exact: true }).first()).toBeVisible()
    await expect(page.locator('body')).not.toContainText(`Category #${cat.id}`)
    await expectNoOverflow(page)
    await testInfo.attach('h-insights-budget-names', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    await page.goto('/h/budgets')
    await expect(page.getByRole('heading', { name: catName })).toBeVisible()
    await expectNoOverflow(page)
    await testInfo.attach('h-budgets-budget-names', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
  })
})

async function apiGet(cookie: string, path: string) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, { headers: { cookie } })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
  return (await res.json()).data
}

async function apiPost(cookie: string, path: string, body: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method: 'POST', headers: { cookie, 'content-type': 'application/json' }, body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path} ${res.status} ${await res.text()}`)
  return (await res.json()).data
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
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
