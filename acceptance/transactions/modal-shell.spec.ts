import { expect, test, type Locator, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// #548: every form modal sits above the mobile top bar (sticky, z-30) and is
// capped to the viewport, so its title is never hidden and its action button
// is always reachable, at desktop width and on a short phone screen where the
// form is taller than the viewport. Throwaway user; runs under both projects.

test.describe('modals stay above the app bar and reachable', () => {
  test('add/new modals on accounts, transactions, budgets, goals', async ({ page }, testInfo) => {
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `modal-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    await api(cookie, '/accounts', {
      name: 'Modal Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD',
    })

    await signIn(page, user.email, user.password)
    const mobile = page.viewportSize()!.width < 768
    // A short phone screen makes every form taller than the viewport.
    if (mobile) await page.setViewportSize({ width: 390, height: 560 })

    // New accounts go through the /accounts/add page, so the account cases
    // use the row's Edit and Update balance modals.
    const cases: { path: string; open: (p: Page) => Locator; dialog: RegExp; title: RegExp; primary: RegExp | null }[] = [
      { path: '/accounts', open: (p) => p.getByRole('button', { name: /^Edit( Modal Checking)?$/ }).first(), dialog: /^Edit "Modal Checking"$/, title: /^Edit "Modal Checking"$/, primary: /^Save$/ },
      { path: '/accounts', open: (p) => p.getByRole('button', { name: 'Update balance for Modal Checking' }), dialog: /^Update balance: Modal Checking$/, title: /^Update balance: Modal Checking$/, primary: /^Save balance$/ },
      { path: '/transactions', open: (p) => p.getByRole('button', { name: /^Add transaction$/ }), dialog: /^Add transaction$/, title: /^Add transaction$/, primary: /^Create$/ },
      { path: '/transactions', open: (p) => p.getByRole('button', { name: /^Import$/ }), dialog: /^Import transactions$/, title: /^Import transactions$/, primary: null },
      { path: '/budgets', open: (p) => p.getByRole('button', { name: /^New budget$/ }).first(), dialog: /^New budget$/, title: /^New budget$/, primary: /^Create$/ },
      { path: '/savings-goals', open: (p) => p.getByRole('button', { name: /^New goal$/ }).first(), dialog: /^New goal$/, title: /^New goal$/, primary: /^Create$/ },
    ]
    for (const c of cases) {
      await page.goto(c.path)
      await c.open(page).click()
      const dialog = page.getByRole('dialog', { name: c.dialog })
      await expect(dialog).toBeVisible()
      await expectWithinViewport(page, dialog, `${c.dialog} panel`)
      await expectOnTop(page, dialog.getByText(c.title).first(), `${c.dialog} title`)
      if (c.primary) {
        const btn = dialog.getByRole('button', { name: c.primary })
        await btn.scrollIntoViewIfNeeded()
        await expectWithinViewport(page, btn, `${c.dialog} primary button`)
        await expectOnTop(page, btn, `${c.dialog} primary button`)
      }
      await testInfo.attach(`modal-${c.dialog.source.replace(/\W+/g, '-')}`, {
        body: await page.screenshot(), contentType: 'image/png',
      })
    }
  })
})

// The element's box lies inside the viewport, so nothing is clipped above
// the top edge or below the bottom edge.
async function expectWithinViewport(page: Page, loc: Locator, what: string) {
  const box = await loc.boundingBox()
  const vp = page.viewportSize()!
  expect(box, `${what} rendered`).not.toBeNull()
  expect(box!.y, `${what} top on screen`).toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height, `${what} bottom on screen`).toBeLessThanOrEqual(vp.height + 1)
  expect(box!.x, `${what} left on screen`).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width, `${what} right on screen`).toBeLessThanOrEqual(vp.width + 1)
}

// The element is what the user would hit at its centre: not covered by the
// sticky top bar or anything else.
async function expectOnTop(page: Page, loc: Locator, what: string) {
  const box = await loc.boundingBox()
  expect(box, `${what} rendered`).not.toBeNull()
  const hit = await loc.evaluate((el, p) => {
    const top = document.elementFromPoint(p.x, p.y)
    return !!top && (el === top || el.contains(top))
  }, { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 })
  expect(hit, `${what} not covered`).toBe(true)
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }),
  })
  if (!res.ok) throw new Error(`signin ${res.status}`)
  return res.headers.get('set-cookie')!.split(';')[0]
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
