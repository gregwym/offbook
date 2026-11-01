import { execFileSync } from 'node:child_process'
import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// #696: deleting a bank-synced row opens the shared dialog (not a native
// confirm), says the bank may send it again and offers Hide instead; a manual
// row gets a plain confirm; a synced row the bank sent back after a delete
// carries a visible note. Bank rows only come from Plaid, so the fixture
// creates ordinary rows through the API and flags them as Plaid rows in the
// isolated QA database (the revive policy itself is covered by the Go sync
// test). Throwaway user; runs under desktop and mobile projects.

test.describe('delete a bank-synced transaction', () => {
  test('dialog warns, Hide keeps the row, Delete removes it, manual stays plain', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `delete-synced-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = (await api(cookie, '/accounts', {
      name: 'Synced Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = async (description: string, amount: string) =>
      (await api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: day, source: 'csv' })).data.id as number
    const hideID = await tx('QADEL Coffee Pending', '-4.50')
    const delID = await tx('QADEL Streaming', '-15.99')
    await tx('QADEL Cash Gift', '-20.00')
    const backID = await tx('QADEL Returned Charge', '-9.00')
    markSynced(hideID, true)
    markSynced(delID, false)
    markSynced(backID, false, true)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    const rowOf = (name: string) => page.getByRole('row').filter({ hasText: name })
    await expect(rowOf('QADEL Streaming')).toBeVisible()

    // A row the bank sent back after the user deleted it says so, in text.
    const note = rowOf('QADEL Returned Charge').getByText(/You deleted this, then your bank sent it again/)
    await expectInViewport(page, note)

    // Synced + pending: warning, Hide instead keeps the row in the list.
    await rowAction(page, 'QADEL Coffee Pending', 'Delete QADEL Coffee Pending')
    let dialog = page.getByRole('dialog', { name: 'Delete this bank transaction?' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('when this pending charge posts')
    await expect(dialog).toContainText('Needs review')
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
    for (const name of ['Cancel', 'Hide instead', 'Delete']) {
      const box = await dialog.getByRole('button', { name, exact: true }).boundingBox()
      expect(box!.height, `${name} hit area`).toBeGreaterThanOrEqual(40)
      await expectInViewport(page, dialog.getByRole('button', { name, exact: true }))
    }
    await expectNoOverflow(page)
    await testInfo.attach('delete-synced-dialog', { body: await page.screenshot(), contentType: 'image/png' })
    await dialog.getByRole('button', { name: 'Hide instead' }).click()
    await expect(dialog).toBeHidden()
    await expect(rowOf('QADEL Coffee Pending').getByText('hidden', { exact: true })).toBeVisible()

    // Synced, posted: Delete removes it.
    await rowAction(page, 'QADEL Streaming', 'Delete QADEL Streaming')
    dialog = page.getByRole('dialog', { name: 'Delete this bank transaction?' })
    await expect(dialog).toContainText('on a later sync')
    await dialog.getByRole('button', { name: 'Delete', exact: true }).click()
    await expect(dialog).toBeHidden()
    await expect(rowOf('QADEL Streaming')).toHaveCount(0)
    await expect(page.getByText('3 total', { exact: true })).toBeVisible()

    // Manual row: plain confirm, no sync warning, Cancel keeps it.
    await rowAction(page, 'QADEL Cash Gift', 'Delete QADEL Cash Gift')
    dialog = page.getByRole('dialog', { name: 'Delete this transaction?' })
    await expect(dialog).toBeVisible()
    await expect(dialog).not.toContainText('bank')
    await expect(dialog.getByRole('button', { name: 'Hide instead' })).toHaveCount(0)
    await testInfo.attach('delete-manual-dialog', { body: await page.screenshot(), contentType: 'image/png' })
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(rowOf('QADEL Cash Gift')).toBeVisible()
    await testInfo.attach('after-delete', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    // The API agrees: hidden row kept, deleted row gone.
    const list = await get(cookie, `/transactions?account_id=${acct}`)
    const ids = list.data.map((t: { id: number }) => t.id)
    expect(ids).toContain(hideID)
    expect(ids).not.toContain(delID)
    expect(list.data.find((t: { id: number }) => t.id === hideID).hidden_at).toBeTruthy()
  })
})

// markSynced turns a seeded row into a Plaid row in the QA database. The QA
// stack publishes Postgres on 15432 (docker-compose.qa.yml).
function markSynced(id: number, pending: boolean, restored = false) {
  const url = process.env.ACCEPTANCE_DATABASE_URL ?? 'postgres://offbook:offbook@localhost:15432/offbook_qa'
  const n = Number(id)
  const sql = `UPDATE transactions SET source = 'plaid', plaid_transaction_id = 'qa-del-${n}', pending = ${pending}` +
    (restored ? ', reviewed_at = NULL, restored_after_user_delete_at = NOW()' : '') + ` WHERE id = ${n}`
  execFileSync('psql', [url, '-v', 'ON_ERROR_STOP=1', '-qc', sql])
}

// A row's secondary actions sit behind "More actions" at every width.
async function rowAction(page: Page, label: string, name: string) {
  await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
  await page.getByRole('menu').getByRole('menuitem', { name, exact: true }).click()
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

async function expectInViewport(page: Page, loc: ReturnType<Page['getByRole']>) {
  await expect(loc).toBeVisible()
  const box = await loc.boundingBox()
  const vw = page.viewportSize()!.width
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'left edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'right edge on screen').toBeLessThanOrEqual(vw)
}
