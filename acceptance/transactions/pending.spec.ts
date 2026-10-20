import { execFileSync } from 'node:child_process'
import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// TX-03 (#513): a transaction the bank hasn't posted yet is marked "pending"
// in the list, and the user is told their edits carry over when it posts.
// Pending rows only come from Plaid, so the fixture creates an ordinary row
// through the API and flags it pending in the isolated QA database (the
// pending -> posted carry-over itself is covered by the Go sync test).
// Throwaway user; runs under desktop and mobile projects.

test.describe('pending transactions', () => {
  test('pending chip renders, explains carry-over and fits on mobile', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `pending-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = (await api(cookie, '/accounts', {
      name: 'Pending Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (description: string, amount: string) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: day, source: 'csv' })
    const pendingID = (await tx('QAPEND Corner Cafe', '-12.50')).data.id as number
    await tx('QAPEND Posted Grocer', '-40.00')
    markPending(pendingID)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    const row = page.getByRole('row').filter({ hasText: 'QAPEND Corner Cafe' })
    const posted = page.getByRole('row').filter({ hasText: 'QAPEND Posted Grocer' })
    await expect(row).toBeVisible()
    const chip = row.getByText('pending', { exact: true })
    await expect(chip).toBeVisible()
    await expect(chip).toHaveAttribute('title', /Your edits will carry over when it posts/)
    await expect(posted.getByText('pending', { exact: true })).toHaveCount(0)
    await expectInViewport(page, chip)
    if (page.viewportSize()!.width < 768) {
      // Touch screens have no hover: the explanation is printed on the row.
      await expectInViewport(page, row.getByText(/Your edits will carry over when it posts/))
    }
    await expectNoOverflow(page)
    await testInfo.attach('pending-row', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })

    // The API carries the flag; ordinary rows default to posted.
    const list = await get(cookie, `/transactions?account_id=${acct}`)
    const apiRow = list.data.find((t: { id: number }) => t.id === pendingID)
    expect(apiRow.pending).toBe(true)
    expect(list.data.find((t: { id: number }) => t.id !== pendingID).pending).toBe(false)
  })
})

// markPending flips the seeded row to pending in the QA database. The QA
// stack publishes Postgres on 15432 (docker-compose.qa.yml).
function markPending(id: number) {
  const url = process.env.ACCEPTANCE_DATABASE_URL ?? 'postgres://offbook:offbook@localhost:15432/offbook_qa'
  execFileSync('psql', [url, '-v', 'ON_ERROR_STOP=1', '-qc', `UPDATE transactions SET pending = true WHERE id = ${Number(id)}`])
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
