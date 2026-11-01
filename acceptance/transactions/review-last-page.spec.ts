import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// #708: reviewing the sole row on page 2 of a 51-row inbox must leave the
// user on a real page of the 50 still waiting — not an empty page that
// claims "all caught up" with no Prev button. Throwaway user, synthetic
// rows; runs under both the desktop and the narrow mobile project.

test.describe('review inbox last page', () => {
  test('reviewing the only row on page 2 shows the remaining 50', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `review-last-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await api(cookie, '/accounts', {
      name: 'Paged Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    for (let i = 1; i <= 51; i++) {
      const day = String(1 + (i % 28)).padStart(2, '0')
      await api(cookie, '/transactions', {
        account_id: acct.data.id, amount: `-${i}.00`, description: `Paged Row ${String(i).padStart(2, '0')}`,
        transaction_date: `2026-09-${day}`, source: 'csv',
      })
    }

    await signIn(page, user.email, user.password)
    await page.goto('/transactions?review=1')
    await expect(page.getByText('51 to review')).toBeVisible()
    const pager = page.getByRole('navigation', { name: 'Pagination' })
    await pager.getByRole('button', { name: 'Next' }).click()
    await expect(pager).toContainText('Page 2 / 2')

    const mark = page.getByRole('button', { name: /^Mark Paged Row \d+ reviewed$/ }).locator('visible=true')
    await expect(mark).toHaveCount(1)
    await testInfo.attach(`${testInfo.project.name}-page2-before`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await mark.click()

    await expect(page.getByText('50 to review')).toBeVisible()
    await expect(page.getByText(/all caught up/i)).toHaveCount(0)
    await expect(page.getByRole('button', { name: /^Mark Paged Row \d+ reviewed$/ }).locator('visible=true')).toHaveCount(50)
    // 50 fit on one page: no pager (#702), and we are on that page.
    await expect(pager).toHaveCount(0)
    await expect(page.getByRole('button', { name: /needs review/i }).first()).toContainText('50')
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-after-review`, { body: await page.screenshot(), contentType: 'image/png' })

    // Undo puts the row back and the inbox count returns to 51.
    await page.getByRole('status').getByRole('button', { name: 'Undo' }).click()
    await expect(page.getByText('51 to review')).toBeVisible()
  })
})

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
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
