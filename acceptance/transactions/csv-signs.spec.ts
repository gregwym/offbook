import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Journey for CSV amount signs (#542). A card export that lists purchases as
// positive is called out in the preview and imports as money out after "Flip
// signs". A bank export with separate debit/credit columns is mapped by hand
// and imports with the right signs. Throwaway user; runs under both the
// desktop and the narrow mobile project.

const visa = [
  'Date,Description,Amount',
  '2026-09-04,Sign Restaurant,54.20',
  '2026-09-05,Sign Grocer,120.35',
  '2026-09-20,Sign Card Payment,-500.00',
].join('\n')

const unionBank = [
  'When,Who,Out,In',
  '2026-09-06,Sign Pharmacy,18.40,',
  '2026-09-15,Sign Payroll,,2100.00',
].join('\n')

test.describe('CSV amount signs', () => {
  test('flip a positive-purchase card export; map separate debit and credit columns', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `signs-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const card = await apiPost(cookie, '/accounts', {
      name: 'Sign Visa', institution_slug: 'manual', account_type: 'credit_card', currency: 'USD',
    })
    const checking = await apiPost(cookie, '/accounts', {
      name: 'Sign Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })

    await signIn(page, user.email, user.password)
    await page.goto('/accounts')

    // Card export with purchases as positive: the preview warns before import.
    await openImport(page, 'Sign Visa')
    await chooseStatement(page, 'visa.csv', visa)
    await page.getByRole('button', { name: 'Preview' }).click()
    const warning = page.getByRole('alert').filter({ hasText: 'Most rows are money in' })
    await expect(warning).toBeVisible()
    await expect(warning).toContainText('Money in 174.55 USD (2)')
    await expectInViewport(page, page.getByRole('checkbox', { name: 'Flip signs' }))
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-sign-warning`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await page.getByRole('checkbox', { name: 'Flip signs' }).check()
    await expect(warning).toBeHidden()
    await expect(page.getByText('Money in 500.00 USD (1) · Money out 174.55 USD (2)')).toBeVisible()
    await expect(page.getByRole('checkbox', { name: 'Flip signs' })).toBeChecked()
    await testInfo.attach(`${testInfo.project.name}-sign-flipped`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await page.getByRole('button', { name: 'Import 3 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()
    expect(await amounts(cookie, card.data.id)).toEqual({
      'Sign Restaurant': -54.2, 'Sign Grocer': -120.35, 'Sign Card Payment': 500,
    })

    // Bank export with unrecognized split headers: map them by hand.
    await openImport(page, 'Sign Checking')
    await chooseStatement(page, 'union.csv', unionBank)
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('Map your columns')).toBeVisible()
    await page.getByRole('radio', { name: 'Separate debit & credit columns' }).check()
    await page.getByRole('combobox', { name: /^Date/ }).selectOption('When')
    await page.getByRole('combobox', { name: /^Money out \(debit\)/ }).selectOption('Out')
    await page.getByRole('combobox', { name: /^Money in \(credit\)/ }).selectOption('In')
    await page.getByRole('combobox', { name: /^Description/ }).selectOption('Who')
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-split-mapping`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText('“Out” as money out and “In” as money in')).toBeVisible()
    await expect(page.getByText('Money in 2,100.00 USD (1) · Money out 18.40 USD (1)')).toBeVisible()
    await page.getByRole('button', { name: 'Import 2 new' }).click()
    await expect(page.getByText('Import transactions', { exact: true })).toBeHidden()
    expect(await amounts(cookie, checking.data.id)).toEqual({ 'Sign Pharmacy': -18.4, 'Sign Payroll': 2100 })

    // Re-importing the card file with the same flip is all duplicates.
    await openImport(page, 'Sign Visa')
    await chooseStatement(page, 'visa.csv', visa)
    await page.getByRole('button', { name: 'Preview' }).click()
    await page.getByRole('checkbox', { name: 'Flip signs' }).check()
    await expect(page.getByText('3 duplicate', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Import 0 new', exact: true })).toBeDisabled()
  })
})

async function amounts(cookie: string, accountID: number) {
  const res = await fetch(`${acceptanceAPIURL()}/transactions?account_id=${accountID}&limit=50`, { headers: { cookie } })
  if (!res.ok) throw new Error(`transactions ${res.status}`)
  const body = await res.json()
  return Object.fromEntries(
    body.data
      .filter((t: { description: string | null }) => t.description?.startsWith('Sign '))
      .map((t: { description: string; amount: string }) => [t.description, Number(t.amount)]),
  )
}

async function openImport(page: Page, account: string) {
  await page.getByRole('button', { name: 'Import transactions' }).first().click()
  await page.locator('select').first().selectOption({ label: account })
}

async function chooseStatement(page: Page, name: string, body: string) {
  await page.locator('input[type=file]').setInputFiles({ name, mimeType: 'text/csv', buffer: Buffer.from(body) })
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

async function expectNoOverflow(page: Page) {
  const o = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
  expect(o, 'no horizontal overflow').toBe(false)
}

async function expectInViewport(page: Page, loc: ReturnType<Page['locator']>) {
  const box = (await loc.boundingBox())!
  const vw = page.viewportSize()!.width
  expect(box.x, 'control starts inside the viewport').toBeGreaterThanOrEqual(0)
  expect(box.x + box.width, 'control ends inside the viewport').toBeLessThanOrEqual(vw)
}
