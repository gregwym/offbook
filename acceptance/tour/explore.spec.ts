import { test, type Page } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Scratch exploratory tour (Engineering, not merged): seed a small realistic
// book and capture every personal surface at desktop and mobile.

test('surface tour', async ({ page }, testInfo) => {
  test.setTimeout(180_000)
  process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
  const user = await createThrowawayUser({ suite: `tour-${testInfo.project.name}` })
  const cookie = await apiSignin(user.email, user.password)
  const cats = (await call(cookie, 'GET', '/categories')).data as { id: number; slug: string }[]
  const cat = (s: string) => cats.find((c) => c.slug === s)?.id
  const chk = (await call(cookie, 'POST', '/accounts', { name: 'Tour Checking', institution_slug: 'manual', account_type: 'checking', currency: 'USD', balance: '3200.00' })).data.id
  const card = (await call(cookie, 'POST', '/accounts', { name: 'Tour Visa Rewards Card', institution_slug: 'manual', account_type: 'credit_card', currency: 'USD' })).data.id
  const d = (n: number) => { const x = new Date(); x.setUTCDate(Math.max(1, x.getUTCDate() - n)); return x.toISOString().slice(0, 10) }
  const tx = (account_id: number, description: string, amount: string, days: number, category_id?: number) =>
    call(cookie, 'POST', '/transactions', { account_id, amount, description, transaction_date: d(days), source: 'csv', category_id })
  await tx(chk, 'ACME PAYROLL', '2500.00', 0, cat('income') ?? cat('salary'))
  await tx(chk, 'WHOLEFDS MKT 10234 SEATTLE WA', '-84.12', 0)
  await tx(card, 'SHELL OIL 5739', '-45.10', 0, cat('gas'))
  await tx(card, 'NETFLIX.COM', '-15.49', 0)
  await tx(chk, 'Coffee Corner', '-4.75', 0)
  await call(cookie, 'POST', '/categorization-rules', { pattern: 'WHOLEFDS', match_type: 'contains', category_id: cat('groceries'), priority: 0 })
  await call(cookie, 'POST', '/categorization-rules', { pattern: 'NETFLIX', match_type: 'contains', category_id: cat('entertainment') ?? cat('subscriptions'), priority: 0, rename_merchant: 'Netflix', add_tags: ['subscription'] })
  await call(cookie, 'POST', '/budgets', { category_id: cat('groceries'), period: 'monthly', amount: '400.00' })
  await call(cookie, 'POST', '/budgets', { category_id: cat('gas'), period: 'monthly', amount: '40.00' })
  await call(cookie, 'POST', '/savings-goals', { name: 'Emergency fund', target_amount: '5000.00' })

  await page.goto('/signin')
  await page.getByLabel(/email/i).fill(user.email)
  await page.getByLabel(/password/i).fill(user.password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/accounts/)

  for (const path of ['/insights', '/accounts', '/accounts/add', `/accounts/${chk}/reconciliation`, '/transactions', '/rules', '/budgets', '/savings-goals', '/settings']) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)
    await shot(page, testInfo, path.replace(/\//g, '_'))
  }
  // Open common dialogs.
  await page.goto('/rules'); await page.waitForLoadState('networkidle')
  await page.getByRole('button', { name: 'New rule' }).first().click()
  await page.waitForTimeout(800)
  await shot(page, testInfo, 'rules-new-dialog')
})

async function shot(page: Page, testInfo: import('@playwright/test').TestInfo, name: string) {
  await testInfo.attach(`${testInfo.project.name}-${name}`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
}

async function apiSignin(email: string, password: string) {
  const res = await fetch(`${acceptanceAPIURL()}/auth/signin`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password }) })
  const set = res.headers.get('set-cookie') ?? ''
  return set.split(';')[0]
}

async function call(cookie: string, method: string, path: string, body?: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, { method, headers: { 'Content-Type': 'application/json', Cookie: cookie }, body: body ? JSON.stringify(body) : undefined })
  const json = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(`${method} ${path} ${res.status} ${JSON.stringify(json)}`)
  return json
}
