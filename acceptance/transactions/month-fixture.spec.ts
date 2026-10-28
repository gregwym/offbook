import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { acceptanceAPIURL } from '../fixtures/env.mjs'
import { expectNoClipping } from '../fixtures/layout.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'

// Daily Driver acceptance month (#526, gates 1/3/5): a seeded, deterministic
// month across checking, savings, a credit card and a brokerage — 200+ rows
// with salary, purchases, card refunds, a fee, a split, a hidden row, a card
// payment, two account-to-account transfers and a buy trade. Insights
// spending (headline + per category), budget actuals and the net-worth
// headline must all agree with the fixture to the cent. Throwaway user;
// runs under desktop and mobile projects.

type Row = {
  account: 'checking' | 'savings' | 'card' | 'brokerage'
  cents: number
  slug?: string
  description: string
  day: number
  hidden?: boolean
  split?: { slug: string; cents: number }[]
  transfer?: string // pair key: both legs of a confirmed transfer share it
}

const PREFIX = 'QAM'

test.describe('acceptance month fixture', () => {
  test('spending, budgets and net worth agree with 200+ seeded rows', async ({ page }, testInfo) => {
    test.setTimeout(180_000)
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const user = await createThrowawayUser({ suite: `month-${testInfo.project.name}` })
    const cookie = await apiSignin(user.email, user.password)
    const cats = (await get(cookie, '/categories')).data as { id: number; slug: string; name: string }[]
    const cat = (slug: string) => {
      const c = cats.find((x) => x.slug === slug)
      if (!c) throw new Error(`category ${slug} missing`)
      return c
    }

    const now = new Date()
    const today = now.getUTCDate()
    const ym = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
    const date = (d: number) => `${ym}-${String(d).padStart(2, '0')}`
    const rows = buildMonth(today)
    expect(rows.length).toBeGreaterThanOrEqual(200)

    // Accounts. Opening balances are not income/spending.
    const opening = { checking: 2000000, savings: 200000 }
    const mk = (name: string, account_type: string, balance?: number) =>
      api(cookie, '/accounts', {
        name, institution_slug: 'manual', account_type, currency: 'USD',
        ...(balance ? { balance: money(balance) } : {}),
      })
    const acct = {
      checking: (await mk('QAM Checking', 'checking', opening.checking)).data.id as number,
      savings: (await mk('QAM Savings', 'savings', opening.savings)).data.id as number,
      card: (await mk('QAM Card', 'credit_card')).data.id as number,
      brokerage: (await mk('QAM Brokerage', 'investment')).data.id as number,
    }

    // Seed rows in small parallel batches.
    const ids: number[] = []
    for (let i = 0; i < rows.length; i += 10) {
      const batch = rows.slice(i, i + 10)
      const created = await Promise.all(batch.map((r) =>
        api(cookie, '/transactions', {
          account_id: acct[r.account], amount: money(r.cents), description: r.description,
          transaction_date: date(r.day), source: 'csv', category_id: r.slug ? cat(r.slug).id : undefined,
        })))
      created.forEach((c, j) => { ids[i + j] = c.data.id })
    }
    const pairs = new Map<string, number[]>()
    for (const [i, r] of rows.entries()) {
      if (r.hidden) await send(cookie, 'PATCH', `/transactions/${ids[i]}`, { hidden: true })
      if (r.split) {
        await send(cookie, 'PUT', `/transactions/${ids[i]}/splits`, {
          splits: r.split.map((s) => ({ category_id: cat(s.slug).id, amount: money(s.cents) })),
        })
      }
      if (r.transfer) pairs.set(r.transfer, [...(pairs.get(r.transfer) ?? []), ids[i]])
    }
    for (const [key, pair] of pairs) {
      expect(pair, `transfer ${key} has two legs`).toHaveLength(2)
      await api(cookie, '/transfers/confirm', { transaction_id_a: pair[0], transaction_id_b: pair[1] })
    }
    // Buy 4 shares at 251.37 from the deposited cash (1,005.48 out of 1,200.00).
    const asset = (await api(cookie, '/assets/ensure', { symbol: 'QAMVTI', kind: 'equity', display_name: 'QAM Total Market' })).data.id
    await api(cookie, `/accounts/${acct.brokerage}/trades`, {
      kind: 'buy', asset_id: asset, quantity: '4', price: '251.37', trade_date: date(today),
    })

    // Expected figures, in integer cents, straight from the fixture.
    const spend: Record<string, number> = {}
    let income = 0
    let balances = opening.checking + opening.savings
    for (const r of rows) {
      balances += r.cents
      if (r.transfer || r.hidden) continue
      if (r.slug === 'income') { income += r.cents; continue }
      for (const part of r.split ?? [{ slug: r.slug!, cents: r.cents }]) {
        spend[part.slug] = (spend[part.slug] ?? 0) - part.cents
      }
    }
    // Trade legs net to zero at the trade price (cash -1,005.48, shares +1,005.48).
    const netWorth = balances
    const spending = Object.values(spend).reduce((a, b) => a + b, 0)
    for (const v of Object.values(spend)) expect(v, 'every category is net spend').toBeGreaterThan(0)

    // API cross-checks: every seeded row is listed, nothing is left proposed
    // as a transfer, and spend-by-category matches the fixture per category.
    const listed = await get(cookie, `/transactions?search=${PREFIX}&limit=1`)
    expect(listed.total).toBe(rows.length)
    const candidates = await get(cookie, '/transfers/candidates')
    expect(candidates.total, 'no accidental transfer proposals').toBe(0)
    const byCat = await get(cookie, '/dashboard/spend-by-category?period=current_month')
    const apiSpend: Record<string, string> = {}
    for (const c of byCat.data ?? []) apiSpend[c.name] = String(c.amount).replace(/^-/, '')
    for (const [slug, cents] of Object.entries(spend)) {
      expect(toCents(apiSpend[cat(slug).name]), `spend ${slug}`).toBe(cents)
    }

    // Budgets on three spending categories, sized so all stay under.
    const budgeted = ['groceries', 'food-and-dining', 'shopping']
    for (const slug of budgeted) {
      await api(cookie, '/budgets', {
        category_id: cat(slug).id, period: 'monthly', amount: money(spend[slug] + 50000),
      })
    }

    // A goal and a rule so their pages have rows to lay out. The rule is not
    // applied, so no seeded figure changes.
    await api(cookie, '/savings-goals', { name: 'QAM Emergency fund with a fairly long goal name', target_amount: money(1000000), account_id: acct.savings })
    await api(cookie, '/categorization-rules', {
      pattern: 'QAM GROCER LONG MERCHANT NAME', match_type: 'contains', category_id: cat('groceries').id, priority: 0,
    })

    await signIn(page, user.email, user.password)

    // Insights: headline net worth + spending, and per-category spend.
    await page.goto('/insights')
    const nw = page.locator('section', { hasText: 'Net worth' }).first()
    await expect(nw).toContainText(fmt(netWorth))
    await expect(nw).not.toContainText('partial')
    await expect(page.getByLabel(/^[A-Z][a-z]+ \d{4} totals$/)).toContainText(`Spending${fmt(spending)}`)
    await expect(page.getByLabel(/^[A-Z][a-z]+ \d{4} totals$/)).toContainText(`Income${fmt(income)}`)
    const band = page.locator('section', { hasText: 'Spending by category' }).first()
    for (const slug of budgeted) {
      await expect(band.getByRole('listitem').filter({ hasText: cat(slug).name }).first()).toContainText(fmt(spend[slug]))
    }
    // #659: the accounts band names an account the way Accounts does.
    const feeding = page.locator('section', { has: page.getByRole('heading', { name: 'Accounts feeding this' }) })
    const checkingRow = feeding.locator('div', { hasText: 'QAM Checking' }).last()
    await expect(checkingRow.getByText('Checking', { exact: true })).toBeVisible()
    await expect(checkingRow.getByText('Manual', { exact: true })).toBeVisible()
    await expect(feeding.locator('div', { hasText: 'QAM Card' }).last().getByText('Credit card', { exact: true })).toBeVisible()
    await expect(feeding.getByText(/^(checking|savings|credit_card|investment|manual)$/)).toHaveCount(0)
    await expectNoClipping(page, 'insights')
    await snap(page, testInfo, 'insights')

    // Budgets: each actual equals the fixture; 500.00 remaining on each.
    await page.goto('/budgets')
    for (const slug of budgeted) {
      const card = page.locator('div.rounded-lg', { has: page.getByRole('heading', { name: cat(slug).name, exact: true }) }).last()
      await expect(card).toContainText(fmt(spend[slug]))
      await expect(card).toContainText('500.00 USD remaining')
    }
    await expectNoClipping(page, 'budgets')
    await snap(page, testInfo, 'budgets')

    // Transactions at volume: the list renders and the page stays in bounds.
    await page.goto('/transactions')
    await expect(page.getByText(`${PREFIX} `).locator('visible=true').first()).toBeVisible()
    await expectNoClipping(page, 'transactions')
    await snap(page, testInfo, 'transactions')

    // Accounts (#655): grouped by kind with exact subtotals, and a net worth
    // that equals the Insights headline to the cent.
    const per = { checking: opening.checking, savings: opening.savings, card: 0, brokerage: 0 }
    for (const r of rows) per[r.account] += r.cents
    await page.goto('/accounts')
    const summary = page.getByLabel('Accounts summary')
    await expect(summary).toContainText(fmt(per.checking + per.savings + per.brokerage))
    // #659: Liabilities is what is owed, as a positive amount; net worth is
    // still assets minus that.
    expect(per.card, 'the card is owed').toBeLessThan(0)
    const owed = summary.locator('div', { has: page.getByText('Liabilities', { exact: true }) })
    await expect(owed.locator('dd')).toHaveText(`${fmt(-per.card)} USD`)
    await expect(summary).not.toContainText(fmt(per.card))
    await expect(summary).toContainText(fmt(netWorth))
    await expect(page.getByLabel('Cash total').locator('visible=true')).toContainText(fmt(per.checking + per.savings))
    await expect(page.getByLabel('Credit cards total').locator('visible=true')).toContainText(fmt(per.card))
    await expect(page.getByLabel('Investments total').locator('visible=true')).toContainText(fmt(per.brokerage))
    await expect(page.getByRole('rowgroup', { name: 'Credit cards' })).toContainText('QAM Card')
    await expect(page.getByText('manual', { exact: true })).toHaveCount(0)

    // #657: an account name opens that account's transactions, with its
    // balance and a way into reconciliation; the filter survives a reload.
    const cardRows = rows.filter((r) => r.account === 'card').length
    const cardList = await get(cookie, `/transactions?account_id=${acct.card}&limit=200`)
    expect(cardList.total, 'card rows incl. transfer legs').toBe(cardRows)
    for (const t of cardList.data) expect(t.account_id).toBe(acct.card)
    await page.getByRole('link', { name: 'QAM Card', exact: true }).click()
    await page.waitForURL(new RegExp(`/transactions\\?account_id=${acct.card}$`))
    const header = page.getByRole('region', { name: 'Account QAM Card' })
    await expect(header.getByLabel('Account balance')).toHaveText(`${fmt(per.card)} USD`)
    await expect(page.getByText(`${cardRows} total`, { exact: true })).toBeVisible()
    await expect(page.getByText(`${PREFIX} `).locator('visible=true').first()).toBeVisible()
    await expectNoClipping(page, 'account-transactions')
    await snap(page, testInfo, 'account-transactions')
    await page.reload()
    await expect(page.getByRole('region', { name: 'Account QAM Card' })).toBeVisible()
    await expect(page.getByText(`${cardRows} total`, { exact: true })).toBeVisible()
    await page.getByRole('region', { name: 'Account QAM Card' }).getByRole('link', { name: 'Reconciliation' }).click()
    await page.waitForURL(new RegExp(`/accounts/${acct.card}/reconciliation$`))
    await expect(page.getByText('QAM Card').locator('visible=true').first()).toBeVisible()
    await snap(page, testInfo, 'account-reconciliation')

    // Every other core page at volume: nothing clipped inside a wrapper (#561).
    for (const [name, path, ready] of [
      ['accounts', '/accounts', 'QAM Checking'],
      ['reconciliation', `/accounts/${acct.checking}/reconciliation`, 'QAM Checking'],
      ['goals', '/savings-goals', 'QAM Emergency fund'],
      ['rules', '/rules', 'QAM GROCER LONG MERCHANT NAME'],
      ['settings', '/settings', 'Settings'],
    ] as const) {
      await page.goto(path)
      await expect(page.getByText(ready).locator('visible=true').first()).toBeVisible()
      await expectNoClipping(page, name)
      await snap(page, testInfo, name)
    }

    // A reload changes nothing.
    await page.goto('/insights')
    await expect(page.locator('section', { hasText: 'Net worth' }).first()).toContainText(fmt(netWorth))
    await expect(page.getByLabel(/^[A-Z][a-z]+ \d{4} totals$/)).toContainText(`Spending${fmt(spending)}`)
  })
})

// buildMonth returns the deterministic fixture. Every non-transfer amount is
// unique in absolute value so the transfer matcher can't pair two unrelated
// rows; only the intended transfer legs mirror each other.
function buildMonth(today: number): Row[] {
  const rand = mulberry32(526)
  const used = new Set<number>()
  const day = () => 1 + Math.floor(rand() * today)
  const take = (cents: number) => {
    if (used.has(Math.abs(cents))) throw new Error(`fixture amount ${cents} reused`)
    used.add(Math.abs(cents))
    return cents
  }
  const fresh = (lo: number, hi: number) => {
    for (;;) {
      const c = lo + Math.floor(rand() * (hi - lo))
      if (!used.has(c)) return take(c)
    }
  }
  const rows: Row[] = []
  const add = (r: Omit<Row, 'description'> & { label: string }) =>
    rows.push({ ...r, description: `${PREFIX} ${r.label} ${rows.length + 1}` })

  // Fixed rows first so the random pass can't collide with them.
  add({ account: 'checking', cents: take(341257), slug: 'income', label: 'Payroll', day: 1 })
  add({ account: 'checking', cents: take(339821), slug: 'income', label: 'Payroll', day: today })
  add({ account: 'checking', cents: take(-1500), slug: 'fees-and-charges', label: 'Overdraft fee', day: day() })
  add({
    account: 'card', cents: take(-23745), slug: 'groceries', label: 'Warehouse club', day: day(),
    split: [{ slug: 'groceries', cents: -15020 }, { slug: 'shopping', cents: -8725 }],
  })
  add({ account: 'card', cents: take(-41233), slug: 'entertainment', label: 'Concert tickets (hidden)', day: day(), hidden: true })
  for (const [key, from, to, cents] of [
    ['card-payment', 'checking', 'card', 250000],
    ['to-savings', 'checking', 'savings', 75000],
    ['to-brokerage', 'checking', 'brokerage', 120000],
  ] as const) {
    take(cents)
    const d = day()
    add({ account: from, cents: -cents, label: `Transfer out ${key}`, day: d, transfer: key })
    add({ account: to, cents, label: `Transfer in ${key}`, day: d, transfer: key })
  }

  const cardCats = ['groceries', 'food-and-dining', 'gas', 'shopping', 'entertainment', 'subscriptions']
  for (let i = 0; i < 150; i++) {
    const slug = cardCats[i % cardCats.length]
    add({ account: 'card', cents: -fresh(300, 18000), slug, label: `Card ${slug}`, day: day() })
  }
  const checkingCats = ['groceries', 'utilities', 'food-and-dining', 'healthcare']
  for (let i = 0; i < 40; i++) {
    const slug = checkingCats[i % checkingCats.length]
    add({ account: 'checking', cents: -fresh(500, 9000), slug, label: `Debit ${slug}`, day: day() })
  }
  for (const slug of ['shopping', 'groceries', 'shopping', 'food-and-dining', 'shopping', 'groceries']) {
    add({ account: 'card', cents: fresh(100, 2500), slug, label: `Refund ${slug}`, day: day() })
  }
  return rows
}

function mulberry32(seed: number) {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

// money renders integer cents as the API's decimal string ("-12.30").
function money(cents: number) {
  const sign = cents < 0 ? '-' : ''
  const abs = Math.abs(cents)
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, '0')}`
}

// fmt mirrors AmountDisplay's grouping ("1,234.56") without the currency.
function fmt(cents: number) {
  const [i, f] = money(cents).replace('-', '').split('.')
  return `${cents < 0 ? '-' : ''}${Number(i).toLocaleString('en-US')}.${f}`
}

function toCents(s: string | undefined) {
  if (s === undefined) return undefined
  const [i, f = ''] = s.split('.')
  return Number(i) * 100 + Number((f + '00').slice(0, 2))
}

async function snap(page: Page, testInfo: TestInfo, name: string) {
  await testInfo.attach(`${testInfo.project.name}-${name}`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
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
  return send(cookie, 'POST', path, body)
}

async function send(cookie: string, method: string, path: string, body: unknown) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method, headers: { 'content-type': 'application/json', cookie }, body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${method} ${path} ${res.status} ${await res.text()}`)
  return res.json()
}

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/signin')
  await page.getByLabel(/email/i).fill(email)
  await page.getByLabel(/password/i).fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/\/insights|\/h\/insights|\/accounts/)
}
