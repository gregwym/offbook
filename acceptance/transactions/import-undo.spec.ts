import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser, withQAClient } from '../fixtures/state.mjs'

// Journey for Undo Import (IMP-01 / #477): a committed CSV import appears in the
// account's import history, can be undone after an explicit confirmation, its
// rows disappear from Transactions, and the same statement can be imported again.
// Throwaway user; runs under both the desktop and the narrow mobile project.

const csv = [
  'Date,Description,Amount',
  '2026-09-01,Undo Coffee,-4.50',
  '2026-09-02,Undo Grocer,-52.10',
  '2026-09-03,Undo Payroll,1200.00',
].join('\n')

test.describe('undo statement import', () => {
  test('undo removes imported rows and allows re-import', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `undo-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await apiPost(cookie, '/accounts', {
      name: 'Undo Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const accountID = acct.data.id as number
    await importCSV(cookie, accountID)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    for (const d of ['Undo Coffee', 'Undo Grocer', 'Undo Payroll']) await expect(page.getByText(d)).toBeVisible()

    await page.goto('/accounts')
    await page.getByRole('button', { name: 'Import transactions' }).first().click()
    const history = page.getByRole('region', { name: 'Import history' })
    await expect(history).toContainText('3 imported')
    await expectNoOverflow(page)

    // Keep leaves the import intact.
    await history.getByRole('button', { name: 'Undo import' }).click()
    await expect(history).toContainText('Remove the 3 transactions')
    await history.getByRole('button', { name: 'Keep' }).click()
    await expect(history.getByRole('button', { name: 'Undo import' })).toBeVisible()

    // Confirmed undo.
    await history.getByRole('button', { name: 'Undo import' }).click()
    await history.getByRole('button', { name: 'Yes, undo import' }).click()
    await expect(history).toContainText(/Undone .* 3 transactions removed/)
    await expect(history.getByRole('button', { name: 'Undo import' })).toHaveCount(0)
    await expectNoOverflow(page)

    await page.goto('/transactions')
    await expect(page.getByText('Undo Coffee')).toHaveCount(0)

    // The same statement can be imported again (no duplicate carry-over).
    const again = await importCSV(cookie, accountID)
    expect(again.data.inserted_count).toBe(3)
    expect(again.data.duplicate_count).toBe(0)
    await page.reload()
    await expect(page.getByText('Undo Coffee')).toBeVisible()
  })

  // #522: an older statement can't be undone while a newer import that set the
  // account's balance still stands (#489). The history says so before any
  // click, names the import to undo first, and unblocks once it's undone.
  test('names the newer statement to undo first, then unblocks the older one', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `undo-order-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await apiPost(cookie, '/accounts', {
      name: 'Order Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const accountID = acct.data.id as number
    await importCSV(cookie, accountID, 'may-statement.csv', [
      'Date,Description,Amount', '2026-05-01,Order Payroll,1000.00', '2026-05-10,Order Rent,-400.00',
    ].join('\n'))
    const june = await importCSV(cookie, accountID, 'june-statement.csv', [
      'Date,Description,Amount', '2026-06-05,Order Grocer,-50.00',
    ].join('\n'))
    // CSV imports carry no closing balance, so stand in for a statement that
    // did: the June import records the (ledger-consistent) 550.00 balance.
    await withQAClient((client) => client.query(
      `INSERT INTO account_balance_observations (user_id, account_id, asset_id, observed_quantity, as_of, source, import_job_id)
       SELECT a.user_id, a.id, a.primary_quote_asset_id, 550, '2026-06-30T00:00:00Z', 'pdf', $2 FROM accounts a WHERE a.id = $1`,
      [accountID, june.data.job_id],
    ))

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')
    await page.getByRole('button', { name: 'Import transactions' }).first().click()
    const history = page.getByRole('region', { name: 'Import history' })
    // Match on the entry's own file-name heading: the blocked note in May's
    // entry also mentions june-statement.csv.
    const entry = (name: string) =>
      history.getByRole('listitem').filter({ has: page.getByText(name, { exact: true }) })
    const mayEntry = entry('may-statement.csv')
    const juneEntry = entry('june-statement.csv')
    await expect(mayEntry).toContainText('To undo this one, first undo “june-statement.csv”')
    await expect(mayEntry.getByRole('button', { name: 'Undo import' })).toHaveCount(0)
    await expect(juneEntry.getByRole('button', { name: 'Undo import' })).toBeVisible()
    await expectInViewport(page, mayEntry.getByText('To undo this one'))
    await expectNoOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('undo-blocked.png'), fullPage: true })

    await juneEntry.getByRole('button', { name: 'Undo import' }).click()
    await juneEntry.getByRole('button', { name: 'Yes, undo import' }).click()
    await expect(juneEntry).toContainText(/Undone .* 1 transaction removed/)
    // The older statement unblocks without a reload.
    await expect(mayEntry).not.toContainText('To undo this one')
    await mayEntry.getByRole('button', { name: 'Undo import' }).click()
    await mayEntry.getByRole('button', { name: 'Yes, undo import' }).click()
    await expect(mayEntry).toContainText(/Undone .* 2 transactions removed/)
    await expectNoOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('undo-unwound.png'), fullPage: true })

    await page.goto('/transactions')
    await expect(page.getByText('Order Rent')).toHaveCount(0)
    await expect(page.getByText('Order Grocer')).toHaveCount(0)
  })

  // #637: the race behind the blocked note. The window opened while the older
  // statement was still undoable; a newer balance-setting import lands before
  // the click. The refused undo refreshes the history in place and names the
  // statement to undo first, with no "close and reopen".
  test('a newer import landing while the window is open is named after the refused undo', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `undo-race-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const acct = await apiPost(cookie, '/accounts', {
      name: 'Race Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })
    const accountID = acct.data.id as number
    await importCSV(cookie, accountID, 'may-statement.csv', [
      'Date,Description,Amount', '2026-05-01,Race Payroll,1000.00', '2026-05-10,Race Rent,-400.00',
    ].join('\n'))

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')
    await page.getByRole('button', { name: 'Import transactions' }).first().click()
    const history = page.getByRole('region', { name: 'Import history' })
    const entry = (name: string) =>
      history.getByRole('listitem').filter({ has: page.getByText(name, { exact: true }) })
    const mayEntry = entry('may-statement.csv')
    await expect(mayEntry.getByRole('button', { name: 'Undo import' })).toBeVisible()

    // Elsewhere (another tab, a sync), June lands and sets the balance.
    const june = await importCSV(cookie, accountID, 'june-statement.csv', [
      'Date,Description,Amount', '2026-06-05,Race Grocer,-50.00',
    ].join('\n'))
    await withQAClient((client) => client.query(
      `INSERT INTO account_balance_observations (user_id, account_id, asset_id, observed_quantity, as_of, source, import_job_id)
       SELECT a.user_id, a.id, a.primary_quote_asset_id, 550, '2026-06-30T00:00:00Z', 'pdf', $2 FROM accounts a WHERE a.id = $1`,
      [accountID, june.data.job_id],
    ))

    await mayEntry.getByRole('button', { name: 'Undo import' }).click()
    await mayEntry.getByRole('button', { name: 'Yes, undo import' }).click()
    const alert = mayEntry.getByRole('alert')
    await expect(alert).toContainText('a newer import into this account has to be undone first')
    await expect(alert).not.toContainText(/close and reopen/i)
    await expect(mayEntry).toContainText('To undo this one, first undo “june-statement.csv”')
    await expect(mayEntry.getByRole('button', { name: /undo import/i })).toHaveCount(0)
    const juneEntry = entry('june-statement.csv')
    await expect(juneEntry.getByRole('button', { name: 'Undo import' })).toBeVisible()
    await expectInViewport(page, mayEntry.getByText('To undo this one'))
    await expectNoOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('undo-race-refused.png'), fullPage: true })

    // Follow the note: undo June, then May, without reopening the window.
    await juneEntry.getByRole('button', { name: 'Undo import' }).click()
    await juneEntry.getByRole('button', { name: 'Yes, undo import' }).click()
    await expect(juneEntry).toContainText(/Undone .* 1 transaction removed/)
    await expect(mayEntry).not.toContainText('To undo this one')
    await mayEntry.getByRole('button', { name: 'Undo import' }).click()
    await mayEntry.getByRole('button', { name: 'Yes, undo import' }).click()
    await expect(mayEntry).toContainText(/Undone .* 2 transactions removed/)
    await expect(mayEntry.getByRole('alert')).toHaveCount(0)
    await expectNoOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('undo-race-unwound.png'), fullPage: true })
  })
})

async function importCSV(cookie: string, accountID: number, fileName = 'undo-statement.csv', body = csv) {
  const form = new FormData()
  form.append('file', new Blob([body], { type: 'text/csv' }), fileName)
  form.append('commit', 'true')
  const res = await fetch(`${acceptanceAPIURL()}/accounts/${accountID}/transactions/import`, {
    method: 'POST', headers: { cookie }, body: form,
  })
  if (!res.ok) throw new Error(`import ${res.status} ${await res.text()}`)
  return res.json()
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
}

async function apiPost(cookie: string, path: string, body: unknown) {
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

async function expectInViewport(page: Page, locator: ReturnType<Page['locator']>) {
  await locator.scrollIntoViewIfNeeded()
  const box = await locator.boundingBox()
  const width = page.viewportSize()!.width
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'starts inside the viewport').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'ends inside the viewport').toBeLessThanOrEqual(width + 1)
}

async function expectNoOverflow(page: Page) {
  const o = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
  expect(o, 'no horizontal overflow').toBe(false)
}
