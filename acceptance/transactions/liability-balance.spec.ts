import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for a manual credit card (#667, Daily Driver gates 3 and 5): the
// balance is typed as the statement shows it (the amount owed, positive),
// stored negative, and the Accounts summary and net worth subtract it. The
// Update-balance dialog pre-fills and confirms in the same terms. Throwaway
// user; runs under desktop and mobile projects.

test.describe('manual credit card balance', () => {
  test('add a card with an amount owed, then update it', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `liability-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await api(cookie, '/accounts', {
      name: 'Owed Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD', balance: '2000.00',
    })

    await signIn(page, user.email, user.password)
    await page.goto('/accounts/add')
    await page.getByRole('button', { name: 'Add manually', exact: true }).click()
    await page.getByLabel('Account name').fill('QA Visa')
    await page.getByLabel('Institution').fill('QA Card Co')
    await page.getByLabel('Type').selectOption({ label: 'Credit card' })
    const owed = page.getByLabel('Amount owed (optional)')
    await expect(owed).toBeVisible()
    await expect(page.getByText(/negative number/)).toHaveCount(0)
    // A negative entry reads as a credit, never silently re-signed.
    await owed.fill('-20')
    await expect(page.getByText(/In credit by/)).toContainText('20.00 USD')
    await owed.fill('450.00')
    await expect(page.getByText('You owe', { exact: false })).toContainText('You owe 450.00 USD.')
    await expectNoOverflow(page)
    await testInfo.attach('add-card-owed', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await page.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL(/\/accounts$/)

    // Stored negative: 2,000.00 - 450.00 = 1,550.00 net worth.
    const tiles = page.getByLabel('Accounts summary')
    await expect(tiles).toContainText('Liabilities')
    await expect(tile(page, 'Liabilities')).toContainText('450.00')
    await expect(tile(page, 'Liabilities')).not.toContainText('credit')
    await expect(tile(page, 'Net worth')).toContainText('1,550.00')
    await expect(tile(page, 'Assets')).toContainText('2,000.00')
    const card = (await get(cookie, '/accounts')).data.find((a: { name: string }) => a.name === 'QA Visa')
    expect(Number(card.balance)).toBe(-450)
    await testInfo.attach('accounts-with-card', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    // Update balance: pre-filled with what is owed, saved negative.
    const row = page.getByRole('row').filter({ hasText: 'QA Visa' })
    await row.getByRole('button', { name: 'Update balance for QA Visa' }).click()
    const dialog = page.getByRole('dialog', { name: /Update balance/ })
    const input = dialog.getByLabel('Amount owed (USD)')
    await expect(input).toHaveValue('450')
    await expect(dialog).toContainText('Enter what your statement says you owe.')
    await input.fill('512.30')
    await expect(dialog.getByText(/You owe/)).toContainText('You owe 512.30 USD.')
    await expectNoOverflow(page)
    await testInfo.attach('update-card-owed', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dialog.getByRole('button', { name: 'Save balance' }).click()
    await expect(dialog.getByRole('status')).toContainText('You now owe 512.30 USD.')
    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(tile(page, 'Liabilities')).toContainText('512.30')
    await expect(tile(page, 'Net worth')).toContainText('1,487.70')
    const after = (await get(cookie, '/accounts')).data.find((a: { name: string }) => a.name === 'QA Visa')
    expect(after.balance.startsWith('-512.3')).toBe(true)
    await testInfo.attach('accounts-after-update', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
  })
})

function tile(page: Page, label: string) {
  return page.getByLabel('Accounts summary').locator('div').filter({ has: page.getByRole('term').filter({ hasText: label }) })
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
