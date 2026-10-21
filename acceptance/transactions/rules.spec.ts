import { expect, test, type Locator, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver gate 1 journey (TX-01): a user drafts a categorization rule,
// sees its live impact before saving, saves it, previews the bulk re-apply
// (nothing changes), applies it, and manual picks survive. A second journey
// (#534) has a rule rename the merchant, so the list and Insights Top
// merchants read one clean name. Throwaway user; runs under both desktop and
// mobile projects. Later journeys cover hide (#536), mark reviewed (#538) and
// run order (#540), tags (#546) and account/amount conditions (#550).

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
    await expect(page.getByRole('heading', { name: 'Rules', exact: true })).toBeVisible()

    // Primary actions must be reachable at any width (not clipped off-screen).
    await expectInViewport(page, page.getByRole('button', { name: 'New rule' }))
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await expect(dialog).toContainText('Fill in pattern and category')
    // Run order is set only in the list (#552): no Priority field here.
    await expect(dialog.getByLabel(/Priority/)).toHaveCount(0)
    await expect(dialog).toContainText('New rules run first; reorder them on the Rules page.')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('QARULE MART')
    await dialog.getByLabel('Category').selectOption({ label: groceries.name })
    // Live impact: 2 would change category, the manual pick is protected.
    await expect(dialog).toContainText('Applies to 2 of')
    await expect(dialog).toContainText('2 would change category')
    await expect(dialog).toContainText('1 manual pick stays as it is')
    await expect(dialog).toContainText('Nothing changes until you save')
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('QARULE MART', { exact: true }).first()).toBeVisible()
    await expectInViewport(page, page.getByRole('button', { name: 'Edit' }).first())
    await expectInViewport(page, page.getByRole('button', { name: 'Delete' }).first())

    // Dry run reports without changing anything.
    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/Preview \(2 of \d+ transactions\): 2 would change category; 1 manual pick stays as it is/)).toBeVisible()
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
    await expect(dialog).toContainText('2 would change category, 2 renamed')
    await expect(dialog).toContainText('Renamed ones show merchant \u201cAmazon\u201d')
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

    // A rename-only change reads as a rename, not a re-categorization (#552).
    await page.getByRole('button', { name: 'Edit' }).first().click()
    const edit = page.locator('div.fixed.inset-0')
    await expect(edit).toContainText('Saving keeps this rule\u2019s place in the run order')
    await edit.getByLabel(/Rename merchant to/).fill('Amazon.com')
    await expect(edit).toContainText('2 would be renamed')
    await expect(edit).not.toContainText('change category')
    await edit.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Renames to Amazon.com')).toBeVisible()
    await page.getByRole('button', { name: 'Preview changes' }).click()
    const banner = page.getByText(/Preview \(2 of \d+ transactions\)/)
    await expect(banner).toContainText('2 would be renamed. Nothing has been changed.')
    await expect(banner).not.toContainText(/re-categorized|category/)
    await testInfo.attach(`${testInfo.project.name}-rename-only-preview`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect(page.getByText(/Updated 2 of \d+ transactions: 2 renamed\./)).toBeVisible()

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
    await expect(dialog).toContainText('2 would change category, 2 hidden')
    await expect(dialog).toContainText('Hidden ones leave spending and budgets')
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
    await rowAction(page, 'QAHIDE SWEEP TO MMF 3', 'Unhide QAHIDE SWEEP TO MMF 3')
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
    await expect(dialog).toContainText('2 would change category, 2 marked reviewed')
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
    // Row actions must sit inside the table's own box, not clipped by its
    // scroll wrapper (the page-level overflow check can't see that).
    const table = page.locator('table').last()
    for (const action of [up, page.getByRole('button', { name: 'Delete' }).last()]) {
      await expectInViewport(page, action)
      await expectInside(table.locator('xpath=..'), action)
    }
    await testInfo.attach(`${testInfo.project.name}-order-before`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(categoryOf).toBe(shopping.id)

    // Move the specific rule above the broad one; the order sticks.
    // Wait for the save itself: the list moves optimistically, and a reload
    // that races the request cancels it.
    await Promise.all([
      page.waitForResponse((r) => r.url().endsWith('/categorization-rules/reorder') && r.ok()),
      up.click(),
    ])
    await expect.poll(patterns).toEqual(['AMAZON PRIME', 'AMAZON'])
    await page.reload()
    await expect.poll(patterns).toEqual(['AMAZON PRIME', 'AMAZON'])
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-order-after`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/Preview \(1 of \d+ transactions\): 1 would change category\./)).toBeVisible()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(categoryOf).toBe(subs.id)

    // A new matching row follows the new order on import.
    const fresh = (await api(cookie, '/transactions', {
      account_id: acct, amount: '-14.99', description: 'QAORD AMAZON PRIME*7Q1', transaction_date: today, source: 'csv',
    })).data
    expect(fresh.category_id).toBe(subs.id)

    // A rule made in the form lands at the top of the list (#552).
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await expect(dialog.getByLabel(/Priority/)).toHaveCount(0)
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('QAORD NEWEST')
    await dialog.getByLabel('Category').selectOption({ label: shopping.name })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect.poll(patterns).toEqual(['QAORD NEWEST', 'AMAZON PRIME', 'AMAZON'])
    await page.reload()
    await expect.poll(patterns).toEqual(['QAORD NEWEST', 'AMAZON PRIME', 'AMAZON'])
  })
})

test.describe('rule adds tags', () => {
  test('matches gain the tags, the user spelling is reused, new rows arrive tagged, spending is unchanged', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-tags-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const travel = (cats.data.find((x: { slug: string }) => x.slug === 'travel') ?? cats.data[0]) as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Tag Rule Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = (description: string, amount: string) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv' })
    await tx('QATAGR UBER TRIP 1', '-18.40')
    await tx('QATAGR UBER TRIP 2', '-22.10')
    const tagged = (await tx('QATAGR Hotel', '-210.00')).data
    // The user already has a "Work trip" tag; the rule should reuse it.
    await patch(cookie, `/transactions/${tagged.id}`, { tags: ['Work trip'] })
    await tx('QATAGR Corner shop', '-9.00')
    const spendBefore = await totalSpend(cookie)

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('UBER')
    await dialog.getByLabel('Category').selectOption({ label: travel.name })
    const tags = dialog.getByLabel('Also add tags (optional)')
    await tags.scrollIntoViewIfNeeded()
    await expectInViewport(page, tags)
    await tags.fill('work TRIP, reimbursable')
    await expect(dialog).toContainText('2 would change category, 2 tagged')
    await expect(dialog).toContainText('Tags: Work trip, reimbursable')
    // The modal scrolls inside, so Create stays on screen even on a phone.
    await expect(dialog.getByRole('button', { name: 'Create', exact: true })).toBeInViewport()
    await testInfo.attach(`${testInfo.project.name}-tags-draft`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Adds tags: Work trip, reimbursable')).toBeVisible()

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(async () => (await get(cookie, '/transactions?tag=reimbursable&limit=50')).total).toBe(2)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-tags-rules-list`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    const again = await api(cookie, '/categorization-rules/apply', {})
    expect(again.data.updated).toBe(0)
    const tagList = await get(cookie, '/transactions/tags')
    expect(tagList.data).toEqual(expect.arrayContaining([{ name: 'Work trip', count: 3 }, { name: 'reimbursable', count: 2 }]))

    // A new matching row arrives already tagged.
    const fresh = (await tx('QATAGR UBER TRIP 3', '-11.00')).data
    expect(fresh.tags).toEqual(['Work trip', 'reimbursable'])

    // A tag chip filters the list to exactly the rule's matches (the filter
    // panel itself is collapsed on mobile, #518).
    await page.goto('/transactions')
    await page.getByRole('button', { name: 'Show transactions tagged reimbursable' }).first().click()
    await expect(page.locator('label', { hasText: /^Tag/ }).locator('select')).toHaveValue('reimbursable')
    await expect(page.getByText(/QATAGR UBER TRIP/).locator('visible=true')).toHaveCount(3)
    const vp = page.viewportSize()!
    if (vp.width < 768) {
      // The filter stack stays collapsed; a chip says what is applied (#554).
      await expect(page.getByRole('button', { name: /^Filters/ })).toHaveAttribute('aria-expanded', 'false')
      await expect(page.getByRole('button', { name: 'Remove filter Tag: reimbursable' })).toBeVisible()
      const first = (await page.locator('tbody tr').first().boundingBox())!
      expect(first.y, 'first row starts on the first screen').toBeLessThan(vp.height)
    }
    await expect(page.getByText('QATAGR Corner shop').locator('visible=true')).toHaveCount(0)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-tags-filtered`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // Tags are labels only: spending is unchanged apart from the new row.
    expect((await totalSpend(cookie)) - spendBefore).toBeCloseTo(11, 2)
  })
})

test.describe('rule keeps the category', () => {
  test('a tag-only rule tags matches, leaves every category and spending total alone, and persists', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-keep-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const shopping = (cats.data.find((x: { slug: string }) => x.slug === 'shopping') ?? cats.data[0]) as { id: number; name: string }
    const acct = (await api(cookie, '/accounts', {
      name: 'Tag Only Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = async (description: string, amount: string) =>
      (await api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv' })).data
    await tx('QAKEEP UBER TRIP 1', '-18.40')
    await tx('QAKEEP UBER TRIP 2', '-22.10')
    await tx('QAKEEP Corner shop', '-9.00')
    // An existing categorizing rule already put the Uber rows in Shopping.
    await api(cookie, '/categorization-rules', { pattern: 'QAKEEP UBER', match_type: 'contains', category_id: shopping.id, priority: 10 })
    await api(cookie, '/categorization-rules/apply', {})
    const byCategory = async () =>
      ((await get(cookie, '/dashboard/spend-by-category?period=current_month')).data ?? [])
        .map((c: { category_id: number | null; amount: string }) => `${c.category_id}:${c.amount}`).sort()
    const spendBefore = await byCategory()
    const categoriesOf = async () =>
      ((await get(cookie, '/transactions?search=QAKEEP&limit=50')).data as { description: string; category_id: number | null; tags?: string[] }[])
        .map((r) => `${r.description}:${r.category_id}`).sort()
    const catsBefore = await categoriesOf()
    expect(catsBefore).toContain(`QAKEEP UBER TRIP 1:${shopping.id}`)

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await page.getByRole('button', { name: 'New rule' }).click()
    const dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('UBER')
    await expect(dialog).toContainText('This rule stops later rules from running on the same transaction.')
    await dialog.getByLabel('Category').selectOption({ label: 'Keep current category' })
    await expect(dialog).toContainText('Matches keep their category.')
    // Keeping the category and nothing else would change nothing.
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(dialog).toContainText('This rule would change nothing.')
    const tags = dialog.getByLabel('Also add tags (optional)')
    await tags.fill('reimbursable')
    await expect(dialog).not.toContainText('This rule would change nothing.')
    await expect(dialog).toContainText('Applies to 2 of')
    await expect(dialog).toContainText('2 would be tagged. Tags: reimbursable. Categories stay as they are.')
    await expect(dialog).not.toContainText('would change category')
    await expect(dialog.getByRole('button', { name: 'Create', exact: true })).toBeInViewport()
    await testInfo.attach(`${testInfo.project.name}-keep-category-draft`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    const row = page.locator('tr', { hasText: 'Adds tags: reimbursable' })
    await expect(row.getByText('Category unchanged')).toBeVisible()

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/Preview \(2 of \d+ transactions\): 2 would be tagged\./)).toBeVisible()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(async () => (await get(cookie, '/transactions?tag=reimbursable&limit=50')).total).toBe(2)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-keep-category-rules-list`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    // Categories and the exact spending-by-category totals are unchanged.
    expect(await categoriesOf()).toEqual(catsBefore)
    expect(await byCategory()).toEqual(spendBefore)
    expect((await api(cookie, '/categorization-rules/apply', {})).data.updated).toBe(0)

    // First match wins: a new Uber row is tagged by the top rule, which keeps
    // its (empty) category, so the Shopping rule below does not run on it.
    const fresh = await tx('QAKEEP UBER TRIP 3', '-11.00')
    expect(fresh.tags).toEqual(['reimbursable'])
    expect(fresh.category_id ?? null).toBeNull()

    await page.reload()
    await expect(page.locator('tr', { hasText: 'Adds tags: reimbursable' }).getByText('Category unchanged')).toBeVisible()
    await page.locator('tr', { hasText: 'Adds tags: reimbursable' }).getByRole('button', { name: /edit/i }).click()
    await expect(page.locator('div.fixed.inset-0').getByLabel('Category')).toHaveValue('keep')
  })
})

test.describe('rule conditions', () => {
  test('account and amount bands split one merchant; other accounts and mid-size rows are left alone', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-cond-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const pick = (slug: string, i: number) => (cats.data.find((x: { slug: string }) => x.slug === slug) ?? cats.data[i]) as { id: number; name: string }
    const shopping = pick('shopping', 0)
    const subs = pick('subscriptions', 1)
    const mk = async (name: string) => (await api(cookie, '/accounts', {
      name, institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id as number
    const joint = await mk('Joint Checking')
    const card = await mk('Travel Card')
    const today = new Date().toISOString().slice(0, 10)
    const tx = async (account_id: number, description: string, amount: string) =>
      (await api(cookie, '/transactions', { account_id, amount, description, transaction_date: today, source: 'csv' })).data
    const small = await tx(joint, 'QACOND AMZN Mktp US 1', '-12.99')
    const big = await tx(joint, 'QACOND AMZN Mktp US 2', '-649.00')
    const mid = await tx(joint, 'QACOND AMZN Mktp US 3', '-89.50')
    const elsewhere = await tx(card, 'QACOND AMZN Mktp US 4', '-12.99')
    const categoryOf = async (id: number) => ((await get(cookie, `/transactions/${id}`)).data.category_id ?? null) as number | null

    await signIn(page, user.email, user.password)
    await page.goto('/rules')

    // Small Amazon charges on the joint account only → Subscriptions.
    await page.getByRole('button', { name: 'New rule' }).click()
    let dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('QACOND AMZN')
    await dialog.getByLabel('Category').selectOption({ label: subs.name })
    const account = dialog.getByRole('combobox', { name: /^Account/ })
    await account.scrollIntoViewIfNeeded()
    await expectInViewport(page, account)
    await account.selectOption({ label: 'Joint Checking' })
    const low = dialog.getByLabel('Lowest amount')
    const high = dialog.getByLabel('Highest amount')
    await expectInViewport(page, high)
    // An inverted band is explained inline and not previewed.
    await low.fill('30')
    await high.fill('20')
    await expect(dialog.getByRole('alert')).toContainText('lower amount must not be more')
    await low.fill('')
    await expect(dialog).toContainText('Applies to 1 of')
    await expect(dialog.getByRole('listitem')).toHaveText(['QACOND AMZN Mktp US 1'])
    await expect(dialog.getByRole('button', { name: 'Create', exact: true })).toBeInViewport()
    await testInfo.attach(`${testInfo.project.name}-conditions-draft`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Only in Joint Checking')).toBeVisible()
    await expect(page.getByText('Only in Joint Checking').locator('xpath=..')).toContainText('Amount up to 20.00 USD')

    // Large ones anywhere → Shopping.
    await page.getByRole('button', { name: 'New rule' }).click()
    dialog = page.locator('div.fixed.inset-0')
    await dialog.getByPlaceholder('e.g. WHOLEFDS').fill('QACOND AMZN')
    await dialog.getByLabel('Category').selectOption({ label: shopping.name })
    await dialog.getByLabel('Lowest amount').fill('$500')
    await expect(dialog).toContainText('Applies to 1 of')
    await dialog.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText('Amount 500.00 USD or more')).toBeVisible()
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-conditions-rules-list`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    await page.getByRole('button', { name: 'Preview changes' }).click()
    await expect(page.getByText(/Preview \(2 of \d+ transactions\): 2 would change category\./)).toBeVisible()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(() => categoryOf(small.id)).toBe(subs.id)
    expect(await categoryOf(big.id)).toBe(shopping.id)
    expect(await categoryOf(mid.id)).toBeNull()
    expect(await categoryOf(elsewhere.id)).toBeNull()
    const again = await api(cookie, '/categorization-rules/apply', {})
    expect(again.data.updated).toBe(0)

    // New rows follow the same conditions on import.
    expect((await tx(joint, 'QACOND AMZN Mktp US 5', '-7.25')).category_id).toBe(subs.id)
    expect((await tx(card, 'QACOND AMZN Mktp US 6', '-7.25')).category_id ?? null).toBeNull()
    expect((await tx(card, 'QACOND AMZN Mktp US 7', '-1200.00')).category_id).toBe(shopping.id)

    // Editing shows the stored band and can drop the account condition.
    const row = page.locator('tbody tr', { hasText: 'Only in Joint Checking' })
    await row.getByRole('button', { name: 'Edit' }).click()
    dialog = page.locator('div.fixed.inset-0')
    await expect(dialog.getByLabel('Highest amount')).toHaveValue('20')
    await dialog.getByRole('combobox', { name: /^Account/ }).selectOption({ label: 'Any account' })
    await dialog.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Only in Joint Checking')).toHaveCount(0)
    await page.getByRole('button', { name: 'Preview changes' }).click()
    await page.getByRole('button', { name: 'Apply now' }).click()
    await expect.poll(() => categoryOf(elsewhere.id)).toBe(subs.id)
  })
})

// #565 (TX-01 revert): a wrong re-apply is undone in one click. Rows come
// back exactly as they were; a row the user changed since is kept; the
// offer survives a reload and disappears once used; spending agrees.
test.describe('undo a rules re-apply', () => {
  test('re-apply, edit one row, reload, undo: the rest come back, the edit stays', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `rules-undo-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const shopping = cats.data.find((x: { slug: string }) => x.slug === 'shopping') as { id: number }
    const gas = cats.data.find((x: { slug: string }) => x.slug === 'gas') as { id: number }
    const acct = (await api(cookie, '/accounts', {
      name: 'Undo Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const today = new Date().toISOString().slice(0, 10)
    const tx = async (description: string, amount: string) =>
      (await api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: today, source: 'csv' })).data
    const a = await tx('QAUNDO MARKET 1', '-12.34')
    const b = await tx('QAUNDO MARKET 2', '-56.78')
    const c = await tx('QAUNDO MARKET 3', '-9.10')
    await tx('Unrelated bakery', '-4.00')
    const spendBefore = await totalSpend(cookie)

    // An over-eager rule: re-categorizes and hides every QAUNDO row.
    await api(cookie, '/categorization-rules', {
      pattern: 'QAUNDO', match_type: 'contains', category_id: shopping.id, hide_matches: true, priority: 10,
    })

    await signIn(page, user.email, user.password)
    await page.goto('/rules')
    await expect(page.getByRole('heading', { name: 'Rules', exact: true })).toBeVisible()
    // Nothing applied yet, so nothing to undo.
    await expect(page.getByRole('button', { name: 'Undo re-apply' })).toHaveCount(0)

    await page.getByRole('button', { name: 'Re-apply to all transactions' }).click()
    await expect(page.getByText(/Updated 3 of \d+ transactions: 3 changed category, 3 hidden\./)).toBeVisible()
    await expect(page.getByText('Last re-apply changed 3 transactions.', { exact: false })).toBeVisible()
    await expectInViewport(page, page.getByRole('button', { name: 'Undo re-apply' }))
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-undo-offered`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })
    expect(await totalSpend(cookie)).toBeCloseTo(spendBefore - 12.34 - 56.78 - 9.10, 2)

    // The user fixes one row by hand after the run.
    await patch(cookie, `/transactions/${c.id}`, { category_id: gas.id })

    // The offer survives a reload.
    await page.reload()
    await expect(page.getByText('Last re-apply changed 3 transactions.', { exact: false })).toBeVisible()
    await page.getByRole('button', { name: 'Undo re-apply' }).click()
    await expect(page.getByText('Undone: 2 transactions are back as they were; 1 transaction changed since then stays as it is.')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Undo re-apply' })).toHaveCount(0)
    await expectNoOverflow(page)
    await testInfo.attach(`${testInfo.project.name}-undo-done`, {
      body: await page.screenshot({ fullPage: true }), contentType: 'image/png',
    })

    const rows = await get(cookie, '/transactions?search=QAUNDO&limit=50')
    const byID = new Map(rows.data.map((t: { id: number }) => [t.id, t]))
    for (const restored of [a, b]) {
      const row = byID.get(restored.id) as { category_id?: number | null; hidden_at?: string; amount: string }
      expect(row.category_id ?? null, `row ${restored.id} category restored`).toBeNull()
      expect(row.hidden_at, `row ${restored.id} unhidden`).toBeUndefined()
      expect(Number(row.amount)).toBe(Number(restored.amount))
    }
    const kept = byID.get(c.id) as { category_id?: number | null; hidden_at?: string }
    expect(kept.category_id).toBe(gas.id)
    expect(kept.hidden_at).toBeTruthy()
    // Spending: the restored rows count again; the kept row stays hidden.
    expect(await totalSpend(cookie)).toBeCloseTo(spendBefore - 9.10, 2)
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

async function patch(cookie: string, path: string, body: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method: 'PATCH', headers: { 'content-type': 'application/json', cookie }, body: JSON.stringify(body),
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

async function expectInside(container: Locator, loc: Locator) {
  const outer = (await container.boundingBox())!
  const box = (await loc.boundingBox())!
  expect(box.x, 'left edge inside container').toBeGreaterThanOrEqual(outer.x)
  expect(box.x + box.width, 'right edge inside container').toBeLessThanOrEqual(outer.x + outer.width)
}

async function expectInViewport(page: Page, loc: ReturnType<Page['getByRole']> | ReturnType<Page['getByLabel']>) {
  const box = await loc.boundingBox()
  const vw = page.viewportSize()!.width
  expect(box, 'element rendered').not.toBeNull()
  expect(box!.x, 'left edge on screen').toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, 'right edge on screen').toBeLessThanOrEqual(vw)
}

// A row's secondary actions sit behind "More actions" at every width
// (#554 phone, #563 desktop).
async function rowAction(page: Page, label: string, name: string) {
  await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
  await page.getByRole('menu').getByRole('menuitem', { name, exact: true }).click()
}
