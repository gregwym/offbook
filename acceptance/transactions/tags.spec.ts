import { expect, test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// TX-01 tags (#544): a user tags two trip expenses (the second in a different
// case, which reuses the first spelling), then finds them again from a tag
// chip and the Tag filter. Tags are metadata only: spending must not move.
// Throwaway user; runs under both desktop and mobile projects.

test.describe('tags journey', () => {
  test('tag rows, reuse the spelling, filter by tag; spending unchanged', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `tags-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = await get(cookie, '/categories')
    const groceries = cats.data.find((x: { slug: string }) => x.slug === 'groceries') as { id: number }
    const acct = (await api(cookie, '/accounts', {
      name: 'Tags Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })).data.id
    const now = new Date()
    const day = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}-01`
    const tx = (description: string, amount: string) =>
      api(cookie, '/transactions', { account_id: acct, amount, description, transaction_date: day, source: 'csv', category_id: groceries.id })
    await tx('QATAG Hotel Roma', '-210.00')
    await tx('QATAG Trattoria', '-64.50')
    await tx('QATAG Corner grocer', '-25.25')
    const spendBefore = await spending(cookie)

    await signIn(page, user.email, user.password)
    await page.goto('/transactions')
    await expect(page.getByText('QATAG Corner grocer').first()).toBeVisible()

    // First tag: typed and added, with the explainer copy visible.
    const vp = page.viewportSize()!
    const mobile = vp.width < 768
    // Tags sits behind "More actions" at both widths (#554 phone, #563 desktop).
    const openTags = async (label: string) => {
      await page.getByRole('button', { name: `More actions for ${label}` }).locator('visible=true').click()
      await page.getByRole('menu').getByRole('menuitem', { name: `Tags for ${label}` }).click()
    }
    // The newest row's actions are on the first screen at both widths.
    await expect(page.getByRole('button', { name: 'More actions for QATAG Corner grocer' }).locator('visible=true')).toBeInViewport()
    if (!mobile) {
      // #563: on desktop an untagged row is one line of controls — Reviewed
      // and More actions — and the date doesn't wrap.
      const row = page.locator('tbody tr').filter({ hasText: 'QATAG Corner grocer' })
      const box = (await row.boundingBox())!
      expect(box.height, 'transaction row height on desktop').toBeLessThanOrEqual(64)
      const review = row.getByRole('button', { name: /QATAG Corner grocer.*review/ }).locator('visible=true')
      const more = row.getByRole('button', { name: 'More actions for QATAG Corner grocer' }).locator('visible=true')
      const rb = (await review.boundingBox())!
      const mb = (await more.boundingBox())!
      expect(Math.abs(rb.y - mb.y), 'Reviewed and More actions share a line').toBeLessThanOrEqual(4)
      await expect(row.getByRole('button', { name: 'Tags for QATAG Corner grocer' })).toHaveCount(0)
      await testInfo.attach(`${testInfo.project.name}-compact-rows`, { body: await page.screenshot(), contentType: 'image/png' })
      await more.click()
      const menu = page.getByRole('menu', { name: 'Actions for QATAG Corner grocer' })
      await expect(menu.getByRole('menuitem')).toHaveText(['Edit details', 'Create rule', 'Split', 'Tags', 'Hide from spending', 'Delete'])
      await expect(menu).toBeInViewport()
      await testInfo.attach(`${testInfo.project.name}-row-menu`, { body: await page.screenshot(), contentType: 'image/png' })
      await page.keyboard.press('Escape')
      await expect(menu).toBeHidden()
    }
    if (mobile) {
      // #554: an untagged row is compact, so several fit on one screen;
      // category and the reviewed toggle stay one tap away.
      const row = page.locator('tbody tr').filter({ hasText: 'QATAG Corner grocer' })
      const box = (await row.boundingBox())!
      expect(box.height, 'transaction row height on a phone').toBeLessThanOrEqual(120)
      await expect(row.getByRole('combobox', { name: 'Category for QATAG Corner grocer' }).locator('visible=true')).toBeVisible()
      await expect(row.getByRole('button', { name: /QATAG Corner grocer.*review/ }).locator('visible=true')).toBeVisible()
      const more = row.getByRole('button', { name: 'More actions for QATAG Corner grocer' }).locator('visible=true')
      const mb = (await more.boundingBox())!
      expect(Math.min(mb.width, mb.height), 'More actions target size').toBeGreaterThanOrEqual(44)
      await testInfo.attach(`${testInfo.project.name}-compact-rows`, { body: await page.screenshot(), contentType: 'image/png' })
      await more.click()
      const menu = page.getByRole('menu', { name: 'Actions for QATAG Corner grocer' })
      await expect(menu.getByRole('menuitem')).toHaveText(['Edit details', 'Create rule', 'Split', 'Tags', 'Hide from spending', 'Delete'])
      await expect(menu.getByRole('menuitem').first()).toBeFocused()
      await expect(menu).toBeInViewport()
      await testInfo.attach(`${testInfo.project.name}-row-menu`, { body: await page.screenshot(), contentType: 'image/png' })
      await page.keyboard.press('Escape')
      await expect(menu).toBeHidden()
      await expect(more).toBeFocused()
    }
    await openTags('QATAG Hotel Roma')
    let dlg = page.getByRole('dialog', { name: 'Tags for QATAG Hotel Roma' })
    await expect(dlg).toContainText('They don’t change spending, budgets or balances.')
    await expect(dlg).toContainText('No tags yet.')
    await dlg.getByLabel('New tag').fill('Rome trip')
    await dlg.getByRole('button', { name: 'Add', exact: true }).click()
    await expect(dlg.getByRole('list', { name: 'Current tags' })).toContainText('Rome trip')
    await testInfo.attach(`${testInfo.project.name}-tag-editor`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await dlg.getByRole('button', { name: 'Save tags' }).click()
    await expect(dlg).toBeHidden()
    await expect(page.getByRole('button', { name: 'Show transactions tagged Rome trip' })).toHaveCount(1)

    // Second tag in another case: typed then saved directly; the server
    // keeps the user's existing spelling, so there is still one tag.
    await openTags('QATAG Trattoria')
    dlg = page.getByRole('dialog', { name: 'Tags for QATAG Trattoria' })
    await expect(dlg.getByRole('button', { name: 'Add tag Rome trip' })).toBeVisible()
    await dlg.getByLabel('New tag').fill('rome TRIP')
    await dlg.getByRole('button', { name: 'Save tags' }).click()
    await expect(dlg).toBeHidden()
    await expect(page.getByRole('button', { name: 'Show transactions tagged Rome trip' })).toHaveCount(2)
    const tags = await get(cookie, '/transactions/tags')
    expect(tags.data).toEqual([{ name: 'Rome trip', count: 2 }])

    // Tag chip -> filtered list with the Tag filter set.
    await page.getByRole('button', { name: 'Show transactions tagged Rome trip' }).first().click()
    await expect(page.getByText('QATAG Corner grocer').locator('visible=true')).toHaveCount(0)
    await expect(page.getByText('QATAG Hotel Roma').locator('visible=true').first()).toBeVisible()
    await expect(page.getByText('QATAG Trattoria').locator('visible=true').first()).toBeVisible()
    const tagFilter = page.locator('label', { hasText: /^Tag/ }).locator('select')
    await expect(tagFilter).toHaveValue('Rome trip')
    await expect(tagFilter.locator('option', { hasText: 'Rome trip (2)' })).toHaveCount(1)
    const chip = page.getByRole('button', { name: 'Remove filter Tag: Rome trip' })
    if (mobile) {
      // #554: the filter stack stays shut; a chip shows the active filter and
      // the list still starts on the first screen.
      await expect(page.getByRole('button', { name: /^Filters/ })).toHaveAttribute('aria-expanded', 'false')
      await expect(chip).toBeInViewport()
      const first = (await page.locator('tbody tr').first().boundingBox())!
      expect(first.y, 'first row starts on the first screen').toBeLessThan(vp.height)
    } else {
      await expect(tagFilter).toBeInViewport()
    }
    await testInfo.attach(`${testInfo.project.name}-tag-filtered`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
    await expectNoOverflow(page)

    // Clearing the filter brings everything back; spending never moved.
    if (mobile) await chip.click()
    else await tagFilter.selectOption('')
    await expect(page.getByText('QATAG Corner grocer').locator('visible=true').first()).toBeVisible()
    expect(await spending(cookie)).toBe(spendBefore)
  })
})

async function spending(cookie: string) {
  const res = await get(cookie, '/dashboard/spend-by-category?period=current_month')
  let total = 0
  for (const c of res.data ?? []) total += Math.abs(Number(c.amount))
  return total.toFixed(2)
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
