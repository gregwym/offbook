import { expect, test } from '@playwright/test'
import { plaidSandboxPublicToken } from './helper.mjs'

test.describe('Plaid sandbox acceptance', () => {
  test('public-token helper mints a sandbox token without Plaid Link', async () => {
    test.skip(!process.env.PLAID_CLIENT_ID || !process.env.PLAID_SECRET, 'Plaid sandbox credentials are not configured')
    await expect(plaidSandboxPublicToken()).resolves.toMatch(/^public-/)
  })
})
