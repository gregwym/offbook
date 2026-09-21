import { expect, test } from '@playwright/test'
import { createThrowawayUser, deleteThrowawayUsers, signin } from '../fixtures/state.mjs'
import {
  attemptSyncTransactions,
  createUpdateLinkToken,
  exchangePublicToken,
  listPlaidItems,
  plaidSandboxPublicToken,
  resetSandboxItemLogin,
  syncPlaidAccounts,
  syncPlaidTransactions,
} from './helper.mjs'

const plaidConfigured = Boolean(process.env.PLAID_CLIENT_ID && process.env.PLAID_SECRET)

// #364: forces a linked item into ITEM_LOGIN_REQUIRED via Plaid's
// sandbox-only reset_login, then asserts Offbook's own re-auth wiring:
// the item flips to the distinct 'reauth_required' status (not generic
// 'error'), a further sync attempt fails instead of retry-storming, and
// the update-mode link-token endpoint the Settings "Reconnect" CTA calls
// still succeeds for the item's owner. Per docs/QA.md, acceptance
// automation must not drive the Plaid Link iframe, so this does not
// complete the actual reconnect — that's covered by the Settings page's
// manual/browser QA pass.
test.describe('Plaid re-auth acceptance', () => {
  test('reset_login flips the item to reauth_required and update-mode link token still works', async () => {
    test.skip(!plaidConfigured, 'Plaid sandbox credentials are not configured')

    await deleteThrowawayUsers('plaid-reauth')
    const user = await createThrowawayUser({ suite: 'plaid-reauth' })
    const cookie = await signin(user.email, user.password)

    try {
      const publicToken = await plaidSandboxPublicToken()
      const exchange = await exchangePublicToken(cookie, publicToken)
      const plaidItemID = exchange.data.item_id
      expect(plaidItemID).toBeTruthy()

      await syncPlaidAccounts(cookie, plaidItemID)
      const baselineSync = await syncPlaidTransactions(cookie, plaidItemID)
      expect(baselineSync.data.failed).toBe(0)

      await resetSandboxItemLogin(cookie, plaidItemID)

      const failedSync = await attemptSyncTransactions(cookie, plaidItemID)
      expect(failedSync.ok).toBe(false)

      const items = await listPlaidItems(cookie)
      const item = items.data.find((i: { plaid_item_id: string }) => i.plaid_item_id === plaidItemID)
      expect(item).toBeTruthy()
      expect(item.last_sync_status).toBe('reauth_required')

      const updateToken = await createUpdateLinkToken(cookie, plaidItemID)
      expect(updateToken.data.link_token).toMatch(/^link-/)
    } finally {
      await deleteThrowawayUsers('plaid-reauth')
    }
  })
})
