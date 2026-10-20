import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1 journey (TX-01): a user drafts a categorization rule,
// sees its live impact before saving, saves it, previews the bulk re-apply
// (nothing changes), applies it, and manual picks survive. A second journey
// (#534) has a rule rename the merchant, so the list and Insights Top
// merchants read one clean name. Throwaway user; runs under both desktop and
// mobile projects. Later journeys cover hide (#536), mark reviewed (#538) and
// run order (#540).

test.describe('categorization rules journey', () => {
  test('rule impact preview, dry-run, apply, manual picks win', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const groceries = cats.data.find((x: { slug: string }) => x.slug === 'groceries') as { id: number; name: string }
    const gas = cats.data.find((x: { slug: string }) => x.slug === 'gas') as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Rules Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = (description: string, amount: string, category_id?: number) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv', category_id })
    await tx('QARULE MART #1', '-10.00')
    await tx('QARULE MART #2', '-20.00')
    const manual = (await tx('QARULE MART MANUAL', '-30.00', gas.id)).data
    await tx('Unrelated coffee', '-5.00')

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await expect(page.getByRole('heading', { name: 'Categorization rules' })).toBeVisible()

    // Primary actions must be reachable at any width (not clipped off-screen).
    await expectInViewport(page, page.getByRole('button', { name: 'New rule' }))
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await expect(dialog).toContainText('Fill in pattern and category')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('QARULE MART')
    await dialog.getByLabel('Category').selectOption({ label: groceries.name })
    // Live impact: 3 match, 2 would change, the manual pick is protected.
    await expect(dialog).toContainText('Would categorize')
    await expect(dialog).toContainText('2 would change')
    await expect(dialog).toContainText('1 manual picks are never overridden')
    await expect(dialog).toContainText('Nothing changes until you save')
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('QARULE MART', { exact: true }).first()).toBeVisible()
    await expectInViewport(page, page.getByRole('button', { name: 'Edit' }).first())
    await expectInViewport(page, page.getByRole('button', { name: 'Delete' }).first())

    // Dry run reports without changing anything.
    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/would be re-categorized/)).toBeVisible()
    await expect(page.getByText(/Nothing has been changed/)).toBeVisible()
    let rows = await get(cookie, `/transactions?search=QARULE&limit=50`)
    expect(rows.data.filter((t: { category_id: number | null }) => t.category_id === groceries.id)).toHaveLength(0)

    // Apply from the preview banner.
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(async () => {
      rows = await get(cookie, `/transactions?search=QARULE&limit=50`)
      return rows.data.filter((t: { category_id: number | null }) => t.category_id === groceries.id).length
    }).toBe(2)
    const still = rows.data.find((t: { id: number }) => t.id === manual.id)
    expect(still.category_id).toBe(gas.id)

    await page.reload()
    await expect(page.getByText('QARULE MART', { exact: true }).first()).toBeVisible()
    await expectNoOverflow(page)
  })
})

test.describe('rule renames the merchant', () => {
  test('one rule cleans raw merchant names across the list and Top merchants', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-rename-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const shopping = (cats.data.find((x: { slug: string }) => x.slug === 'shopping') ?? cats.data[0]) as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Rename Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = (description: string, merchant_name: string, amount: string) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, merchant_name, transaction_date: today, source: 'csv' })
    await tx('AMZN MKTP US*2K4', 'AMZN Mktp US', '-12.50')
    await tx('AMZN Digital*9X1', 'AMZN Digital', '-30.00')
    await tx('Corner cafe', 'Corner Cafe', '-5.00')

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('AMZN')
    await dialog.getByLabel('Category').selectOption({ label: shopping.name })
    const rename = dialog.getByLabel(/Rename merchant to/)
    await expectInViewport(page, rename)
    await rename.fill('Amazon')
    await expect(dialog).toContainText('2 would show merchant \u201cAmazon\u201d')
    await testInfo.attach(`${testInfo.project.name}-rename-draft`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Renames to Amazon')).toBeVisible()

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(async () => {
      const rows = await get(cookie, '/transactions?search=AMZN&limit=50')
      return rows.data.filter((t: { merchant_name: string | null }) => t.merchant_name === 'Amazon').length
    }).toBe(2)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-rename-rules-list`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // Re-applying is a no-op.
    const again = await api(cookie, '/categorization-rules/apply', {})
    expect(again.data.updated).toBe(0)

    await page.goto('/transactions')
    await expect(page.locator('tr', { hasText: 'AMZN MKTP US*2K4' })).toContainText('Amazon')
    await expect(page.locator('tr', { hasText: 'AMZN MKTP US*2K4' })).not.toContainText('AMZN Mktp US')
    await expect(page.locator('tr', { hasText: 'Corner cafe' })).toContainText('Corner Cafe')
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-rename-transactions`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // Top merchants lists one Amazon line summing both rows (12.50 + 30.00).
    await page.goto('/insights')
    const merchants = page.locator('div', { has: page.getByRole('heading', { name: 'Top merchants' }) }).last()
    const amazon = merchants.getByRole('listitem').filter({ hasText: 'Amazon' })
    await expect(amazon).toHaveCount(1)
    await expect(amazon).toContainText('×2')
    await expect(amazon).toContainText('42.50')
    await expect(merchants).not.toContainText('AMZN')
    await merchants.scrollIntoViewIfNeeded()
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-rename-top-merchants`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
  })
})

test.describe('rule hides matches', () => {
  test('one rule hides recurring noise from spending; new rows arrive hidden; unhide is respected', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-hide-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const shopping = (cats.data.find((x: { slug: string }) => x.slug === 'shopping') ?? cats.data[0]) as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Hide Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = (description: string, amount: string, category_id?: number) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv', ...(category_id ? { category_id } : {}) })
    await tx('QAHIDE SWEEP TO MMF', '-500.00')
    await tx('QAHIDE SWEEP TO MMF 2', '-300.00')
    await tx('QAHIDE Corner shop', '-40.00', shopping.id)
    // Spending counts all three before the rule (840).
    expect(await totalSpend(cookie)).toBe(840)

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('SWEEP')
    await dialog.getByLabel('Category').selectOption({ label: shopping.name })
    const hide = dialog.getByLabel('Also hide matching transactions')
    await expectInViewport(page, hide)
    await hide.check()
    await expect(dialog).toContainText('2 would be hidden from spending and budgets')
    await testInfo.attach(`${testInfo.project.name}-hide-draft`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Hides matches')).toBeVisible()

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(() => totalSpend(cookie)).toBe(40)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-hide-rules-list`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    const again = await api(cookie, '/categorization-rules/apply', {})
    expect(again.data.updated).toBe(0)

    // A new matching row arrives hidden; the ledger still lists it.
    const fresh = (await tx('QAHIDE SWEEP TO MMF 3', '-200.00')).data
    expect(fresh.hidden_at).toBeTruthy()
    expect(await totalSpend(cookie)).toBe(40)

    await page.goto('/transactions')
    await expect(page.getByText('QAHIDE SWEEP TO MMF 3').locator('visible=true').first()).toBeVisible()
    await expect(page.locator('span:visible', { hasText: /^hidden$/ })).toHaveCount(3)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-hide-transactions`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // Unhiding one row counts it again.
    await page.getByRole('button', { name: 'Unhide QAHIDE SWEEP TO MMF 3' }).click()
    await expect.poll(() => totalSpend(cookie)).toBe(240)
    await expect(page.locator('span:visible', { hasText: /^hidden$/ })).toHaveCount(2)
  })
})

test.describe('rule marks matches reviewed', () => {
  test('trusted recurring rows skip Needs review; new rows arrive reviewed; one can be sent back', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-review-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const income = (cats.data.find((x: { slug: string }) => x.slug === 'income') ?? cats.data[0]) as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Review Rule Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = (description: string, amount: string) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv' })
    await tx('QAREV ACME PAYROLL 1', '2500.00')
    await tx('QAREV ACME PAYROLL 2', '2500.00')
    await tx('QAREV Corner shop', '-40.00')
    expect(await reviewCount(cookie)).toBe(3)

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('PAYROLL')
    await dialog.getByLabel('Category').selectOption({ label: income.name })
    const mark = dialog.getByLabel('Also mark matching transactions reviewed')
    await mark.scrollIntoViewIfNeeded()
    await expectInViewport(page, mark)
    await mark.check()
    await expect(dialog).toContainText('2 would be marked reviewed')
    await testInfo.attach(`${testInfo.project.name}-review-draft`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Marks reviewed')).toBeVisible()

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(() => reviewCount(cookie)).toBe(1)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-review-rules-list`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    const again = await api(cookie, '/categorization-rules/apply', {})
    expect(again.data.updated).toBe(0)

    // A new matching row arrives already reviewed.
    const fresh = (await tx('QAREV ACME PAYROLL 3', '2500.00')).data
    expect(fresh.reviewed_at).toBeTruthy()
    expect(await reviewCount(cookie)).toBe(1)

    // The Needs review inbox holds only the untrusted row.
    await page.goto('/transactions')
    await page.getByRole('button', { name: /needs review/i }).first().click()
    await expect(page.getByText('QAREV Corner shop').locator('visible=true').first()).toBeVisible()
    await expect(page.getByText(/QAREV ACME PAYROLL/).locator('visible=true')).toHaveCount(0)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-review-inbox`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // Any one can still be sent back to review.
    await api(cookie, '/transactions/review', { ids: [fresh.id], reviewed: false })
    expect(await reviewCount(cookie)).toBe(2)
  })
})

test.describe('rule run order', () => {
  test('overlapping rules: the top rule wins, and moving one up changes the winner', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-order-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const pick = (slug: string) => (cats.data.find((x: { slug: string }) => x.slug === slug) ?? cats.data[0]) as { id: number; name: string }
    const shopping = pick('shopping')
    const subs = pick('subscriptions')
    const acct = (await api(cookie, '/accounts', {
      name: 'Order Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const prime = (await api(cookie, '/transactions', {
      account_id: acct, amount: '-14.99', description: 'QAORD AMAZON PRIME*2K4', transaction_date: today, source: 'csv',
    })).data
    // The broad rule was made last, so it sits on top and wins.
    await api(cookie, '/categorization-rules', { pattern: 'AMAZON PRIME', match_type: 'contains', category_id: subs.id, priority: 10 })
    await api(cookie, '/categorization-rules', { pattern: 'AMAZON', match_type: 'contains', category_id: shopping.id, priority: 20 })
    const categoryOf = async () => (await get(cookie, `/transactions/${prime.id}`)).data.category_id as number

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await expect(page.getByText(/Rules run top to bottom; the first match wins/)).toBeVisible()
    const patterns = () => page.locator('tbody tr td:nth-child(2)').allInnerTexts()
    await expect.poll(patterns).toEqual(['AMAZON', 'AMAZON PRIME'])
    await expect(page.locator('tbody tr').first().locator('td').first()).toHaveText('1')
    await expect(page.getByRole('button', { name: 'Move "AMAZON" up' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Move "AMAZON PRIME" down' })).toBeDisabled()
    const up = page.getByRole('button', { name: 'Move "AMAZON PRIME" up' })
    await expectInViewport(page, up)
    await expectInViewport(page, page.getByRole('button', { name: 'Delete' }).last())
    await testInfo.attach(`${testInfo.project.name}-order-before`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(categoryOf).toBe(shopping.id)

    // Move the specific rule above the broad one; the order sticks.
    await up.click()
    await expect.poll(patterns).toEqual(['AMAZON PRIME', 'AMAZON'])
    await page.reload()
    await expect.poll(patterns).toEqual(['AMAZON PRIME', 'AMAZON'])
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-order-after`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/1 of \d+ transactions would be re-categorized/)).toBeVisible()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(categoryOf).toBe(subs.id)

    // A new matching row follows the new order on import.
    const fresh = (await api(cookie, '/transactions', {
      account_id: acct, amount: '-14.99', description: 'QAORD AMAZON PRIME*7Q1', transaction_date: today, source: 'csv',
    })).data
    expect(fresh.category_id).toBe(subs.id)
  })
})

async function reviewCount(cookie: string) {
  const res = await get(cookie, '/transactions?reviewed=false&limit=1')
  return res.total as number
}

async function totalSpend(cookie: string) {
  const res = await get(cookie, '/dashboard/spend-by-category?period=current_month')
  return (res.data ?? []).reduce((acc: number, c: { amount: string }) => acc + Math.abs(Number(c.amount)), 0)
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

async function expectInViewport(page: Page, loc: ReturnType<Page['getByRole']> | ReturnType<Page['getByLabel']>) {
  const box = await loc.boundingBox()
  const vw = page.viewportSize()!.width
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'left edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'right edge on screen').toBeLessThanOrEqual(vw)
}
