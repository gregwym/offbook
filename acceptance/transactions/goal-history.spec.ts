import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// #682: a goal's contribution history. Log a contribution and a withdrawal
// through the UI, open History, and see each one with its date, signed
// amount and the total after it, newest first. The list refreshes after a
// new contribution and survives reload. Throwaway user; desktop + mobile.

test.describe('goal contribution history', () => {
  test('log, withdraw, read history with running totals, persists', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `goal-history-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await api(cookie, '/savings-goals', { name: 'Emergency fund', target_amount: '1000.00' })

    await signIn(page, user.email, user.password)
    await page.goto('/savings-goals')
    const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: 'Emergency fund', exact: true }) }).last()
    await expect(card).toBeVisible()

    // #686: the card's actions name their goal and are thumb-sized on phones.
    const minTarget = testInfo.project.name.includes('mobile') ? 40 : 32
    for (const action of ['Edit', 'Delete']) {
      const btn = card.getByRole('button', { name: `${action} Emergency fund goal`, exact: true })
      const box = await btn.boundingBox()
      expect(box, `${action} is rendered`).not.toBeNull()
      expect(box!.width, `${action} hit area width`).toBeGreaterThanOrEqual(minTarget)
      expect(box!.height, `${action} hit area height`).toBeGreaterThanOrEqual(minTarget)
    }

    // Empty state before anything is logged.
    const toggle = card.getByRole('button', { name: /^History/ })
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await toggle.click()
    await expect(card.getByText(/No contributions yet/)).toBeVisible()
    await testInfo.attach('goal-history-empty', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    const log = async (amount: string) => {
      await card.getByRole('button', { name: /Log contribution/ }).click()
      const dialog = page.getByRole('dialog')
      await dialog.getByLabel(/Amount/).fill(amount)
      await dialog.getByRole('button', { name: 'Log', exact: true }).click()
      await expect(dialog).toBeHidden()
    }
    await log('250.25')
    // The open history reloads after a contribution.
    const list = card.getByRole('list', { name: 'Contribution history for Emergency fund' })
    await expect(list.getByRole('listitem')).toHaveCount(1)
    await log('-100.50')
    await expect(list.getByRole('listitem')).toHaveCount(2)

    const items = list.getByRole('listitem')
    await expect(items.nth(0)).toContainText('Withdrawal')
    await expect(items.nth(0)).toContainText('−100.50')
    await expect(items.nth(0)).toContainText('Total 149.75')
    await expect(items.nth(1)).toContainText('Contribution')
    await expect(items.nth(1)).toContainText('+250.25')
    await expect(items.nth(1)).toContainText('Total 250.25')
    // The card's headline total matches the newest running total.
    await expect(card).toContainText('149.75')
    await expectNoOverflow(page)
    await testInfo.attach('goal-history-open', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    // Persisted: reload and reopen.
    await page.reload()
    await card.getByRole('button', { name: /^History/ }).click()
    await expect(list.getByRole('listitem')).toHaveCount(2)
    await expect(list.getByRole('listitem').nth(0)).toContainText('Total 149.75')
    await expectNoOverflow(page)
    await testInfo.attach('goal-history-reloaded-open', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await card.getByRole('button', { name: /Hide history/ }).click()
    await expect(list).toBeHidden()
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
