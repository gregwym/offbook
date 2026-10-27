import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser, withQAClient } from '../fixtures/state.mjs'
import { randomBytes } from 'node:crypto'

// Recovery journey (#641): a linked bank that needs re-auth must be visible
// from the pages a daily user actually opens, with a path to Reconnect —
// not only from Settings. Needs no Plaid credentials: the reauth_required
// item and its account are seeded through SQL, exactly the state #364's
// sync leaves behind on ITEM_LOGIN_REQUIRED. Driving the Plaid Link iframe
// stays out of scope (docs/QA.md), so this stops at the Reconnect click.
// Throwaway user; runs under desktop and mobile projects.

const BANK = 'QA Reauth Bank'

test.describe('reconnect a bank that needs re-auth', () => {
  test('banner on every page leads to the Settings reconnect action', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `reconnect-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await api(cookie, '/accounts', {
      name: 'QA Linked Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const itemID = `qa-reauth-${randomBytes(6).toString('hex')}`
    await withQAClient(async (client) => {
      await client.query(
        `INSERT INTO plaid_items (user_id, plaid_item_id, access_token_enc, institution_name, status,
                                  last_sync_status, last_synced_at, last_sync_error)
         VALUES ($1, $2, '\\x00'::bytea, $3, 'active', 'reauth_required', NOW() - INTERVAL '2 days',
                 'ITEM_LOGIN_REQUIRED: the login details of this item have changed')`,
        [user.id, itemID, BANK],
      )
      await client.query('UPDATE accounts SET plaid_item_id = $1 WHERE id = $2', [itemID, acct.data.id])
    })

    await signIn(page, user.email, user.password)
    await page.goto('/insights')
    await expect(page.getByRole('heading', { name: 'Insights', level: 1 })).toBeVisible()
    await expect(page.getByText('Loading…')).toHaveCount(0)
    const banner = page.getByRole('status').filter({ hasText: 'needs you to sign in again' })
    await expect(banner).toContainText(`${BANK} needs you to sign in again`)
    await expect(banner).toContainText('Last synced')
    await expect(banner).toContainText('stop updating until you reconnect')
    await expect(page.getByText(/ITEM_LOGIN_REQUIRED/)).toHaveCount(0)
    await expectInViewport(page, banner.getByRole('link', { name: 'Reconnect' }))
    await expectNoClipping(page, 'insights with reconnect banner')
    await testInfo.attach('reconnect-banner-insights', { body: await page.screenshot(), contentType: 'image/png' })

    // The same prompt follows the user to the daily review.
    await page.goto('/transactions')
    await expect(banner).toContainText(BANK)

    // Accounts: the pill is a way to fix it, not a raw-error expander.
    await page.goto('/accounts')
    const row = page.getByRole('row').filter({ hasText: 'QA Linked Checking' })
    const pill = row.getByRole('link', { name: /Needs reconnect/ }).filter({ visible: true })
    await expect(pill).toHaveCount(1)
    await expectNoClipping(page, 'accounts with reauth pill')
    await testInfo.attach('reconnect-accounts-pill', { body: await page.screenshot(), contentType: 'image/png' })
    await pill.click()

    // Settings: lands on Linked Institutions, no duplicate banner, plain copy.
    await page.waitForURL(/\/settings#linked-institutions$/)
    await expect(banner).toHaveCount(0)
    const reconnect = page.getByRole('button', { name: `Reconnect ${BANK}` })
    await expectInViewport(page, reconnect)
    await expect(page.getByText(/Your bank needs you to sign in again · last synced/)).toBeVisible()
    // The row wraps its actions below on narrow screens rather than
    // squeezing the bank name to an ellipsis.
    const name = page.getByText(BANK, { exact: true })
    expect(await name.evaluate((el) => el.scrollWidth <= el.clientWidth), 'bank name not truncated').toBe(true)
    await expect(page.getByText(/ITEM_LOGIN_REQUIRED/)).toHaveCount(0)
    await expectNoClipping(page, 'settings linked institutions')
    await testInfo.attach('reconnect-settings', { body: await page.screenshot(), contentType: 'image/png' })

    // The seeded item has no real Plaid token, so Link can't open here. The
    // click must still settle on a readable message, not a stuck button.
    await reconnect.click()
    await expect(reconnect).toHaveText('Reconnect')
    await expect(page.getByRole('alert')).toContainText('Couldn’t start the reconnect')
    await testInfo.attach('reconnect-settings-error', { body: await page.screenshot(), contentType: 'image/png' })

    // Back on Insights the banner is still there until the bank is fixed.
    await page.goto('/insights')
    await expect(banner).toContainText(BANK)
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

async function expectInViewport(page: Page, loc: ReturnType<Page['getByRole']>) {
  await expect(loc).toBeVisible()
  const box = await loc.boundingBox()
  const vp = page.viewportSize()!
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'left edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'right edge on screen').toBeLessThanOrEqual(vp.width)
  expect(box!.y, 'top edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height, 'bottom edge on screen').toBeLessThanOrEqual(vp.height)
}
