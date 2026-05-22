import { expect, test } from '@playwright/test'
import { createThrowawayUser, deleteThrowawayUsers, signin } from '../fixtures/state.mjs'
import {
  exchangePublicToken,
  listAccounts,
  listTransactions,
  plaidSandboxPublicToken,
  syncPlaidAccounts,
  syncPlaidTransactions,
} from './helper.mjs'

const plaidConfigured = Boolean(process.env.PLAID_CLIENT_ID && process.env.PLAID_SECRET)

test.describe('Plaid sandbox acceptance', () => {
  test('public-token helper mints a sandbox token without Plaid Link', async () => {
    test.skip(!plaidConfigured, 'Plaid sandbox credentials are not configured')
    await expect(plaidSandboxPublicToken()).resolves.toMatch(/^public-/)
  })

  test('exchange, sync, and re-sync produce accounts and no duplicate transactions', async () => {
    test.skip(!plaidConfigured, 'Plaid sandbox credentials are not configured')

    await deleteThrowawayUsers('plaid')
    const user = await createThrowawayUser({ suite: 'plaid' })
    const cookie = await signin(user.email, user.password)

    try {
      const publicToken = await plaidSandboxPublicToken()
      const exchange = await exchangePublicToken(cookie, publicToken)
      const plaidItemID = exchange.data.item_id
      expect(plaidItemID).toBeTruthy()

      const accountSync = await syncPlaidAccounts(cookie, plaidItemID)
      expect(accountSync.data.created + accountSync.data.updated).toBeGreaterThan(0)

      const accounts = await listAccounts(cookie)
      expect(accounts.total).toBeGreaterThan(0)
      expect(accounts.data.some((account: { plaid_item_id?: string }) => account.plaid_item_id === plaidItemID)).toBe(true)

      const firstTxnSync = await syncPlaidTransactions(cookie, plaidItemID)
      expect(firstTxnSync.data.inserted).toBeGreaterThan(0)
      expect(firstTxnSync.data.failed).toBe(0)

      const transactionsAfterFirstSync = await listTransactions(cookie)
      expect(transactionsAfterFirstSync.total).toBeGreaterThan(0)

      const secondTxnSync = await syncPlaidTransactions(cookie, plaidItemID)
      expect(secondTxnSync.data.inserted).toBe(0)
      expect(secondTxnSync.data.failed).toBe(0)

      const transactionsAfterSecondSync = await listTransactions(cookie)
      expect(transactionsAfterSecondSync.total).toBe(transactionsAfterFirstSync.total)
    } finally {
      await deleteThrowawayUsers('plaid')
    }
  })
})
