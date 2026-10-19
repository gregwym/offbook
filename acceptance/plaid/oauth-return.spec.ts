import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { createThrowawayUser } from '../fixtures/state.mjs'

// #502: Plaid OAuth institutions leave the app and come back to
// /plaid/oauth-return as a full-page redirect. docs/QA.md forbids driving the
// real Plaid Link iframe, and Plaid sandbox only exercises the redirect when a
// dashboard-registered redirect_uri is set (CI/QA register none), so this
// journey stubs the Link CDN script instead. The stub reproduces the OAuth
// shape: the first Link session navigates the whole tab to the return URL;
// the return page must re-init Link with the stashed token plus
// `receivedRedirectUri`, then finish exchange → syncs. Plaid API calls are
// routed in the browser because the QA stack has no Plaid credentials.
// Runs under both the desktop and the mobile project.

const STUB_PLAID = `
window.Plaid = {
  create(cfg) {
    window.__plaidInits = (window.__plaidInits || []).concat([{ token: cfg.token, receivedRedirectUri: cfg.receivedRedirectUri || null }])
    setTimeout(() => cfg.onLoad && cfg.onLoad(), 0)
    return {
      open() {
        if (!cfg.receivedRedirectUri) {
          // First Link session: the bank's OAuth sign-in takes over the tab.
          window.location.assign('/plaid/oauth-return?oauth_state_id=stub-oauth-state')
          return
        }
        const behavior = window.localStorage.getItem('stub.plaid') || 'success'
        if (behavior === 'success') cfg.onSuccess('public-stub-oauth', { institution: { name: 'Platypus OAuth Bank' } })
        else if (behavior === 'invalid-token') cfg.onExit({ error_code: 'INVALID_LINK_TOKEN', error_type: 'INVALID_INPUT', error_message: 'expired', display_message: '' }, {})
        else cfg.onExit(null, {})
      },
      exit() {}, destroy() {}, submit() {},
    }
  },
}`

test.describe('Plaid OAuth return', () => {
  test('new link and re-auth resume after the bank redirect; failures offer a way back', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `oauth-${testInfo.project.name}` })
    const calls = await stubPlaid(page)

    await signIn(page, user.email, user.password)

    // 1. New link from Add account → bank redirect → back, accounts imported.
    await page.goto('/accounts/add')
    await page.getByRole('button', { name: 'Connect a bank' }).click()
    await page.waitForURL(/\/accounts$/)
    expect(calls).toEqual(['link-token:new', 'exchange', 'sync-accounts:item-oauth', 'sync-transactions:item-oauth'])
    expect(await page.evaluate(() => window.sessionStorage.length)).toBe(0)

    // 2. Re-auth from Settings (update mode) → bank redirect → back to Settings, no exchange.
    calls.length = 0
    await page.goto('/settings')
    await page.getByRole('button', { name: /Reconnect Platypus OAuth Bank/ }).click()
    await page.waitForURL(/\/settings$/)
    await expect(page.getByText('Platypus OAuth Bank').first()).toBeVisible()
    expect(calls).toEqual(['link-token:update:item-reauth', 'sync-accounts:item-reauth', 'sync-transactions:item-reauth'])
    await shot(page, testInfo, 'settings-after-reauth')

    // 3. Return page re-inits Link with the stored token + receivedRedirectUri.
    await seedPending(page, { mode: 'new', linkToken: 'link-stub-new', expiration: inOneHour() })
    await page.evaluate(() => window.localStorage.setItem('stub.plaid', 'cancel'))
    await page.goto('/plaid/oauth-return?oauth_state_id=cancel-state')
    await expect(page.getByRole('alert')).toHaveText('Bank sign-in didn’t finish')
    const returnInits = await page.evaluate(() => (window as unknown as { __plaidInits: Array<{ token: string; receivedRedirectUri: string }> }).__plaidInits)
    expect(returnInits.at(-1)).toEqual({ token: 'link-stub-new', receivedRedirectUri: expect.stringContaining('oauth_state_id=cancel-state') })
    await expectNoOverflow(page)
    await shot(page, testInfo, 'return-cancelled')
    await page.getByRole('link', { name: 'Back to Add account' }).click()
    await page.waitForURL(/\/accounts\/add$/)

    // 4. Expired stored token: no Link, clear message, retry path.
    await seedPending(page, { mode: 'new', linkToken: 'link-stub-old', expiration: new Date(Date.now() - 1000).toISOString() })
    await page.goto('/plaid/oauth-return?oauth_state_id=old')
    await expect(page.getByRole('alert')).toHaveText('Your bank sign-in session expired')
    await expectNoOverflow(page)
    await shot(page, testInfo, 'return-expired')

    // 5. Plaid rejects the token on re-init (update mode) → expired, back to Settings.
    await seedPending(page, { mode: 'update', linkToken: 'link-stub-bad', expiration: inOneHour(), itemId: 'item-reauth' })
    await page.evaluate(() => window.localStorage.setItem('stub.plaid', 'invalid-token'))
    await page.goto('/plaid/oauth-return?oauth_state_id=bad')
    await expect(page.getByRole('alert')).toHaveText('Your bank sign-in session expired')
    await expect(page.getByRole('link', { name: 'Back to Settings' })).toBeVisible()

    // 6. Opened in a different tab / storage gone.
    await page.evaluate(() => window.sessionStorage.clear())
    await page.goto('/plaid/oauth-return?oauth_state_id=lost')
    await expect(page.getByRole('alert')).toHaveText('We couldn’t find a bank sign-in to finish')
    await shot(page, testInfo, 'return-missing')

    // 7. Exchange fails → error, nothing linked, back to Add account.
    await page.evaluate(() => window.localStorage.setItem('stub.plaid', 'success'))
    calls.length = 0
    await seedPending(page, { mode: 'new', linkToken: 'link-stub-new', expiration: inOneHour() })
    await page.route('**/api/v1/plaid/link/exchange', (r) =>
      r.fulfill({ status: 502, json: { error: 'Plaid could not finish linking. Try again.', code: 'PLAID_ERROR' } }),
    )
    await page.goto('/plaid/oauth-return?oauth_state_id=fail')
    await expect(page.getByRole('alert')).toHaveText('Couldn’t link your bank')
    await expect(page.getByText('Plaid could not finish linking. Try again.')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Back to Add account' })).toBeVisible()
    await expectNoOverflow(page)
    await shot(page, testInfo, 'return-exchange-failed')
  })
})

async function stubPlaid(page: Page) {
  const calls: string[] = []
  await page.route('https://cdn.plaid.com/link/v2/stable/link-initialize.js', (r) =>
    r.fulfill({ status: 200, contentType: 'application/javascript', body: STUB_PLAID }),
  )
  await page.route('**/api/v1/plaid/link/token', async (r) => {
    const body = r.request().postDataJSON() as { plaid_item_id?: string } | null
    calls.push(body?.plaid_item_id ? `link-token:update:${body.plaid_item_id}` : 'link-token:new')
    await r.fulfill({ json: { data: { link_token: 'link-stub-1', expiration: inOneHour() } } })
  })
  await page.route('**/api/v1/plaid/link/exchange', async (r) => {
    calls.push('exchange')
    await r.fulfill({ json: { data: { item_id: 'item-oauth' } } })
  })
  await page.route(/\/api\/v1\/plaid\/items\/([^/]+)\/sync-(accounts|transactions)$/, async (r) => {
    const m = /items\/([^/]+)\/sync-(accounts|transactions)$/.exec(r.request().url())!
    calls.push(`sync-${m[2]}:${m[1]}`)
    await r.fulfill({ json: { data: {} } })
  })
  await page.route('**/api/v1/plaid/items', (r) =>
    r.fulfill({
      json: {
        data: [{
          id: 1, user_id: 1, plaid_item_id: 'item-reauth', institution_id: 'ins_127287',
          institution_name: 'Platypus OAuth Bank', status: 'active', last_synced_at: null,
          last_sync_error: 'ITEM_LOGIN_REQUIRED', last_sync_status: 'reauth_required',
          unresolved_sync_errors: 0, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
        }],
        total: 1,
      },
    }),
  )
  return calls
}

async function seedPending(page: Page, p: Record<string, unknown>) {
  await page.evaluate((v) => window.sessionStorage.setItem('offbook.plaid.pendingLink', JSON.stringify(v)), p)
}

function inOneHour() {
  return new Date(Date.now() + 60 * 60 * 1000).toISOString()
}

async function shot(page: Page, testInfo: TestInfo, name: string) {
  await testInfo.attach(`${testInfo.project.name}-${name}`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
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
