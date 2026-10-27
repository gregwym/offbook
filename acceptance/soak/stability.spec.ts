import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { acceptanceAPIURL, personaPassword } from '../fixtures/env.mjs'
import { createThrowawayUser } from '../fixtures/state.mjs'
import {
  exchangePublicToken,
  plaidSandboxPublicToken,
  syncPlaidAccounts,
  syncPlaidTransactions,
} from '../plaid/helper.mjs'

// Stability soak (#631, Daily Driver gate 2): one synthetic book survives
// repeated statement re-imports, an overlapping import, Plaid Sandbox
// re-syncs and "Apply rules" — and, with SOAK_PHASE=seed then verify around a
// backend restart (qa-journeys restart_between=true), a process restart —
// with byte-identical money figures. Opt-in only: playwright.config ignores
// soak/ unless QA_SOAK=1, so neither the required smoke nor qa-journeys runs
// it. Desktop project only: the soak is about the ledger, not the layout.
//
// SOAK_PHASE: unset/all = seed + rounds; seed = same, then save state;
// verify = reload the saved user, re-assert the baseline, one more round.
// The state file holds the throwaway email and ids, never a password (it is
// re-derived from QA_SECRET), and soak-result.json holds synthetic figures.

const PREFIX = 'SOAK'
const ROUNDS = 3
const phase = (process.env.SOAK_PHASE ?? 'all') as 'all' | 'seed' | 'verify'
const plaidConfigured = Boolean(process.env.PLAID_CLIENT_ID && process.env.PLAID_SECRET)

type AccountKey = 'checking' | 'savings' | 'card' | 'brokerage'
type Row = { account: AccountKey; cents: number; description: string; day: number; slug?: string }
type Figures = Record<string, string | number | boolean>
type State = {
  email: string
  accounts: Record<AccountKey, number>
  budgetID: number
  manualTxnID: number
  manualCategoryID: number
  plaidItemID: string | null
  baseline: Figures
}
type Round = { round: number; label: string; ms: number; imports: Record<string, { new: number; duplicate: number; inserted: number }>; plaid: string; rules_updated: number }

test.describe('stability soak', () => {
  test('re-import, re-sync, apply rules and restart leave every figure unchanged', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chromium', 'the soak runs once, on desktop')
    test.setTimeout(600_000)
    process.env.OFFBOOK_ROLE = process.env.OFFBOOK_ROLE ?? 'qa'
    const started = Date.now()
    const stateFile = join(testInfo.config.rootDir, '.soak', 'state.json')
    const rounds: Round[] = []
    const fixture = buildFixture()

    let state: State
    let cookie: string
    if (phase === 'verify') {
      state = JSON.parse(readFileSync(stateFile, 'utf8')) as State
      cookie = await apiSignin(state.email, personaPassword(state.email))
      // Straight after the restart, before touching anything.
      expect(await figures(cookie, state), 'figures after backend restart').toEqual(state.baseline)
      rounds.push(await round(cookie, state, fixture, ROUNDS + 1, 'after restart'))
      expect(await figures(cookie, state), 'figures after the post-restart round').toEqual(state.baseline)
    } else {
      const seedStart = Date.now()
      state = await seed(fixture, testInfo)
      cookie = await apiSignin(state.email, personaPassword(state.email))
      const seedMs = Date.now() - seedStart
      for (let n = 1; n <= ROUNDS; n++) {
        rounds.push(await round(cookie, state, fixture, n, n === 1 ? 'first (overlap lands)' : 'repeat'))
        const now = await figures(cookie, state)
        if (n === 1) {
          state.baseline = now
          checkAgainstFixture(now, fixture, state)
        } else {
          expect(now, `figures after round ${n}`).toEqual(state.baseline)
        }
      }
      rounds.unshift({ round: 0, label: 'seed', ms: seedMs, imports: {}, plaid: state.plaidItemID ? 'linked' : 'skipped', rules_updated: 0 })
      if (phase === 'seed') {
        mkdirSync(dirname(stateFile), { recursive: true })
        writeFileSync(stateFile, JSON.stringify(state, null, 2))
      }
    }

    // The manual category survives every rules pass and re-sync.
    const manual = (await get(cookie, `/transactions/${state.manualTxnID}`)).data
    expect(manual.category_id, 'manual category kept').toBe(state.manualCategoryID)

    // The screen tells the same story as the API.
    await signIn(page, state.email, personaPassword(state.email))
    await page.goto('/insights')
    const nw = page.locator('section', { hasText: 'Net worth' }).first()
    await expect(nw).toContainText(fmt(String(state.baseline.net_worth)))
    await snap(page, testInfo, `insights-${phase}`)
    await page.goto('/budgets')
    await expect(page.getByText(fmt(String(state.baseline.budget_spent))).first()).toBeVisible()
    await snap(page, testInfo, `budgets-${phase}`)

    const out = join(testInfo.config.rootDir, 'reports', 'soak', phase, 'soak-result.json')
    mkdirSync(dirname(out), { recursive: true })
    writeFileSync(out, JSON.stringify({
      phase,
      sha: process.env.TESTED_SHA ?? null,
      plaid: state.plaidItemID ? 'sandbox item re-synced every round' : 'skipped: no Plaid Sandbox credentials in this environment',
      rows: { fixture_seed: fixture.seedCount, fixture_total: fixture.rows.length },
      rounds,
      figures: state.baseline,
      duration_ms: Date.now() - started,
    }, null, 2))
  })
})

// seed creates the book: four accounts, three statements imported through the
// CSV path, confirmed transfers (incl. a card payment), a split, a pending
// row, rules, one manual category, a buy trade, a budget and, when
// configured, a Plaid Sandbox item.
async function seed(fx: Fixture, testInfo: TestInfo): Promise<State> {
  const user = await createThrowawayUser({ suite: `soak-${testInfo.project.name}` })
  const cookie = await apiSignin(user.email, user.password)
  const cats = (await get(cookie, '/categories')).data as { id: number; slug: string }[]
  const cat = (slug: string) => {
    const c = cats.find((x) => x.slug === slug)
    if (!c) throw new Error(`category ${slug} missing`)
    return c.id
  }

  // Rules first, so imports and every later "Apply rules" agree.
  for (const slug of fx.slugs) {
    await post(cookie, '/categorization-rules', { pattern: `${PREFIX} ${slug} `, match_type: 'contains', category_id: cat(slug), priority: 0 })
  }

  const mk = async (name: string, account_type: string, cents?: number) =>
    (await post(cookie, '/accounts', {
      name, institution_slug: 'manual', account_type, currency: 'USD', ...(cents ? { balance: money(cents) } : {}),
    })).data.id as number
  const accounts: Record<AccountKey, number> = {
    checking: await mk('SOAK Checking', 'checking', fx.opening.checking),
    savings: await mk('SOAK Savings', 'savings', fx.opening.savings),
    card: await mk('SOAK Card', 'credit_card'),
    brokerage: await mk('SOAK Brokerage', 'investment'),
  }
  for (const key of Object.keys(fx.statements) as AccountKey[]) {
    const res = await importCSV(cookie, accounts[key], fx.statements[key].seed)
    expect(res.inserted_count, `seed ${key}`).toBe(fx.statements[key].seedRows)
  }

  const byDesc = async (description: string) => {
    const list = await get(cookie, `/transactions?search=${encodeURIComponent(description)}&limit=5`)
    const hit = (list.data as { id: number; description: string }[]).find((t) => t.description === description)
    if (!hit) throw new Error(`seeded row ${description} not found`)
    return hit.id
  }
  for (const key of fx.transfers) {
    await post(cookie, '/transfers/confirm', {
      transaction_id_a: await byDesc(`${PREFIX} Transfer out ${key}`),
      transaction_id_b: await byDesc(`${PREFIX} Transfer in ${key}`),
    })
  }
  await send(cookie, 'PUT', `/transactions/${await byDesc(fx.split.description)}/splits`, {
    splits: fx.split.parts.map((p) => ({ category_id: cat(p.slug), amount: money(p.cents) })),
  })
  markPending(await byDesc(fx.pending))
  const manualTxnID = await byDesc(fx.manual.description)
  const manualCategoryID = cat(fx.manual.slug)
  await send(cookie, 'PATCH', `/transactions/${manualTxnID}`, { category_id: manualCategoryID })

  // 4 shares at 251.37 out of the 1,200.00 moved into the brokerage.
  const asset = (await post(cookie, '/assets/ensure', { symbol: 'SOAKVTI', kind: 'equity', display_name: 'SOAK Total Market' })).data.id
  await post(cookie, `/accounts/${accounts.brokerage}/trades`, { kind: 'buy', asset_id: asset, quantity: '4', price: '251.37', trade_date: fx.date(fx.today) })

  const budgetID = (await post(cookie, '/budgets', { category_id: cat('groceries'), period: 'monthly', amount: '5000.00' })).data.id as number

  let plaidItemID: string | null = null
  if (plaidConfigured) {
    plaidItemID = (await exchangePublicToken(cookie, await plaidSandboxPublicToken())).data.item_id as string
    await syncPlaidAccounts(cookie, plaidItemID)
    await syncPlaidTransactions(cookie, plaidItemID)
  }
  return { email: user.email, accounts, budgetID, manualTxnID, manualCategoryID, plaidItemID, baseline: {} }
}

// round is one pass of everything a daily driver repeats.
async function round(cookie: string, state: State, fx: Fixture, n: number, label: string): Promise<Round> {
  const start = Date.now()
  const imports: Round['imports'] = {}
  for (const key of Object.keys(fx.statements) as AccountKey[]) {
    const s = fx.statements[key]
    const again = await importCSV(cookie, state.accounts[key], s.seed)
    expect(again.new_count, `round ${n} ${key}: same statement is all duplicate`).toBe(0)
    expect(again.duplicate_count).toBe(s.seedRows)
    imports[`${key}-same`] = { new: again.new_count, duplicate: again.duplicate_count, inserted: again.inserted_count }
    if (s.overlap) {
      const ov = await importCSV(cookie, state.accounts[key], s.overlap)
      const fresh = n === 1 ? s.overlapNew : 0
      expect(ov.inserted_count, `round ${n} ${key}: overlap inserts only new rows`).toBe(fresh)
      expect(ov.duplicate_count).toBe(s.overlapRows - fresh)
      imports[`${key}-overlap`] = { new: ov.new_count, duplicate: ov.duplicate_count, inserted: ov.inserted_count }
    }
  }
  let plaid = 'skipped'
  if (state.plaidItemID) {
    const sync = (await syncPlaidTransactions(cookie, state.plaidItemID)).data
    expect(sync.inserted, `round ${n}: Plaid re-sync inserts nothing`).toBe(0)
    expect(sync.failed).toBe(0)
    plaid = `inserted ${sync.inserted}, updated ${sync.updated ?? 0}`
  }
  const applied = (await post(cookie, '/categorization-rules/apply', {})).data
  // Round 1 may categorize the overlap's new rows; afterwards nothing moves.
  if (n > 1) expect(applied.updated, `round ${n}: Apply rules changes nothing`).toBe(0)
  return { round: n, label, ms: Date.now() - start, imports, plaid, rules_updated: applied.updated }
}

// figures are the money values the soak holds fixed, as the API's exact
// decimal strings.
async function figures(cookie: string, state: State): Promise<Figures> {
  const out: Figures = {}
  out.transaction_count = (await get(cookie, '/transactions?limit=1')).total
  out.soak_row_count = (await get(cookie, `/transactions?search=${PREFIX}&limit=1`)).total
  const accounts = (await get(cookie, '/accounts?limit=200')).data as { id: number; name: string; balance: string }[]
  for (const a of accounts.sort((x, y) => x.id - y.id)) out[`balance:${a.name}`] = a.balance
  const summary = (await get(cookie, '/dashboard/summary?period=current_month')).data
  out.net_worth = summary.net_worth
  out.net_worth_complete = summary.net_worth_complete
  out.income = summary.income
  out.spending = summary.spending
  const cash = (await get(cookie, '/dashboard/cash-flow?months=1')).data as { month: string; inflow: string; outflow: string; net: string }[]
  const month = cash[cash.length - 1]
  out.month = month.month
  out.month_inflow = month.inflow
  out.month_outflow = month.outflow
  out.month_net = month.net
  const byCat = (await get(cookie, '/dashboard/spend-by-category?period=current_month')).data as { name: string; amount: string }[]
  for (const c of byCat.sort((x, y) => x.name.localeCompare(y.name))) out[`spend:${c.name}`] = c.amount
  const budget = (await get(cookie, `/budgets/${state.budgetID}/spend`)).data
  out.budget_spent = budget.spent
  out.budget_remaining = budget.remaining
  const sum = (await get(cookie, '/budgets/summary')).data
  out.budgets_total_spending = sum.total_spending
  return out
}

// checkAgainstFixture ties the baseline to the fixture itself, so a soak
// that is stable but wrong still fails.
function checkAgainstFixture(f: Figures, fx: Fixture, state: State) {
  expect(f.soak_row_count, 'every fixture row landed once').toBe(fx.rows.length)
  expect(f.net_worth_complete).toBe(true)
  for (const key of ['checking', 'savings', 'card'] as const) {
    expect(toCents(String(f[`balance:SOAK ${cap(key)}`])), `${key} balance`).toBe(fx.balance[key])
  }
  expect(toCents(String(f.budget_spent)), 'groceries budget actual').toBe(fx.groceries)
  if (!state.plaidItemID) {
    // Trade legs net to zero at the trade price, so net worth is the cash.
    expect(toCents(String(f.net_worth)), 'net worth').toBe(fx.netWorth)
  }
}

type Statement = { seed: string; seedRows: number; overlap?: string; overlapRows: number; overlapNew: number }
type Fixture = ReturnType<typeof buildFixture>

// buildFixture returns the deterministic book. Every non-transfer amount is
// unique in absolute value so the transfer matcher cannot pair two unrelated
// rows; only the intended legs mirror each other.
function buildFixture() {
  const now = new Date()
  const today = now.getUTCDate()
  const ym = `${now.getUTCFullYear()}-${String(now.getUTCMonth() + 1).padStart(2, '0')}`
  const date = (d: number) => `${ym}-${String(d).padStart(2, '0')}`
  const rand = mulberry32(631)
  const used = new Set<number>()
  const day = () => 1 + Math.floor(rand() * today)
  const take = (c: number) => {
    if (used.has(Math.abs(c))) throw new Error(`fixture amount ${c} reused`)
    used.add(Math.abs(c))
    return c
  }
  const fresh = (lo: number, hi: number) => {
    for (;;) {
      const c = lo + Math.floor(rand() * (hi - lo))
      if (!used.has(c)) return take(c)
    }
  }
  const seedRows: Row[] = []
  const laterRows: Row[] = []
  const add = (list: Row[], account: AccountKey, cents: number, label: string, slug?: string, d = day()) =>
    list.push({ account, cents, slug, day: d, description: `${PREFIX} ${slug ? `${slug} ` : ''}${label} ${seedRows.length + laterRows.length + 1}` })

  add(seedRows, 'checking', take(341257), 'Payroll', 'income', 1)
  add(seedRows, 'checking', take(-1500), 'Overdraft fee', 'fees-and-charges')
  add(seedRows, 'card', take(-23745), 'Warehouse club', 'groceries')
  const splitRow = seedRows[seedRows.length - 1]
  const transfers = ['card-payment', 'to-savings', 'to-brokerage'] as const
  for (const [key, from, to, cents] of [
    ['card-payment', 'checking', 'card', 250000],
    ['to-savings', 'checking', 'savings', 75000],
    ['to-brokerage', 'checking', 'brokerage', 120000],
  ] as const) {
    take(cents)
    const d = day()
    seedRows.push({ account: from, cents: -cents, day: d, description: `${PREFIX} Transfer out ${key}` })
    seedRows.push({ account: to, cents, day: d, description: `${PREFIX} Transfer in ${key}` })
  }
  const cardCats = ['groceries', 'food-and-dining', 'gas', 'shopping', 'entertainment', 'subscriptions']
  for (let i = 0; i < 160; i++) add(seedRows, 'card', -fresh(300, 18000), 'purchase', cardCats[i % cardCats.length])
  const debitCats = ['groceries', 'utilities', 'healthcare']
  for (let i = 0; i < 30; i++) add(seedRows, 'checking', -fresh(500, 9000), 'debit', debitCats[i % debitCats.length])
  for (const slug of ['shopping', 'groceries', 'shopping', 'food-and-dining']) add(seedRows, 'card', fresh(100, 2500), 'refund', slug)
  const pending = seedRows.find((r) => r.account === 'checking' && r.slug === 'utilities')!.description
  const manualRow = seedRows.find((r) => r.account === 'card' && r.slug === 'groceries' && r !== splitRow)!

  // The overlapping statements: their tail of the seed plus new rows.
  add(laterRows, 'checking', take(339821), 'Payroll', 'income', today)
  for (let i = 0; i < 4; i++) add(laterRows, 'checking', -fresh(500, 9000), 'debit', debitCats[i % debitCats.length])
  for (let i = 0; i < 24; i++) add(laterRows, 'card', -fresh(300, 18000), 'purchase', cardCats[i % cardCats.length])
  add(laterRows, 'card', fresh(100, 2500), 'refund', 'groceries')

  const csv = (rows: Row[]) => ['Date,Description,Amount', ...rows.map((r) => `${date(r.day)},${r.description},${money(r.cents)}`)].join('\n')
  const statements = {} as Record<AccountKey, Statement>
  for (const key of ['checking', 'savings', 'card', 'brokerage'] as const) {
    const s = seedRows.filter((r) => r.account === key)
    const l = laterRows.filter((r) => r.account === key)
    const tail = s.slice(-Math.min(20, s.length))
    statements[key] = {
      seed: csv(s), seedRows: s.length,
      ...(l.length ? { overlap: csv([...tail, ...l]) } : {}),
      overlapRows: tail.length + l.length, overlapNew: l.length,
    }
  }

  const rows = [...seedRows, ...laterRows]
  const opening = { checking: 2000000, savings: 200000 }
  const balance = { checking: opening.checking, savings: opening.savings, card: 0, brokerage: 0 }
  for (const r of rows) balance[r.account] += r.cents
  // Groceries spending: the rules' groceries rows (refunds net against it),
  // with the split row counting only its 150.20 groceries part and the row
  // moved by hand to entertainment left out.
  let groceries = 0
  for (const r of rows) {
    if (r.slug !== 'groceries' || r === manualRow) continue
    groceries -= r === splitRow ? -15020 : r.cents
  }
  return {
    today, date, rows, seedCount: seedRows.length, statements, transfers, opening, balance, groceries,
    netWorth: balance.checking + balance.savings + balance.card + balance.brokerage,
    slugs: [...new Set(rows.map((r) => r.slug).filter((s): s is string => Boolean(s)))],
    split: { description: splitRow.description, parts: [{ slug: 'groceries', cents: -15020 }, { slug: 'shopping', cents: -8725 }] },
    pending,
    manual: { description: manualRow.description, slug: 'entertainment' },
  }
}

// markPending flips one imported row to pending in the QA database (pending
// rows otherwise only come from Plaid). The QA stack publishes Postgres on
// 15432 (docker-compose.qa.yml).
function markPending(id: number) {
  const url = process.env.ACCEPTANCE_DATABASE_URL ?? 'postgres://offbook:offbook@localhost:15432/offbook_qa'
  execFileSync('psql', [url, '-v', 'ON_ERROR_STOP=1', '-qc', `UPDATE transactions SET pending = true WHERE id = ${Number(id)}`])
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

function cap(s: string) {
  return s[0].toUpperCase() + s.slice(1)
}

// money renders integer cents as the API's decimal string ("-12.30").
function money(cents: number) {
  const sign = cents < 0 ? '-' : ''
  const abs = Math.abs(cents)
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, '0')}`
}

// toCents parses an exact decimal string; it refuses sub-cent digits so a
// rounding drift cannot hide.
function toCents(s: string) {
  const neg = s.startsWith('-')
  const [i, f = ''] = s.replace('-', '').split('.')
  if (/[1-9]/.test(f.slice(2))) throw new Error(`${s} has sub-cent digits`)
  const c = Number(i) * 100 + Number((f + '00').slice(0, 2))
  return neg ? -c : c
}

// fmt mirrors AmountDisplay's grouping ("1,234.56") without the currency.
function fmt(s: string) {
  const c = toCents(s)
  const [i, f] = money(c).replace('-', '').split('.')
  return `${c < 0 ? '-' : ''}${Number(i).toLocaleString('en-US')}.${f}`
}

async function importCSV(cookie: string, accountID: number, body: string) {
  const form = new FormData()
  form.append('file', new Blob([body], { type: 'text/csv' }), 'statement.csv')
  form.append('commit', 'true')
  const res = await fetch(`${acceptanceAPIURL()}/accounts/${accountID}/transactions/import`, { method: 'POST', headers: { cookie }, body: form })
  if (!res.ok) throw new Error(`import ${accountID} ${res.status} ${await res.text()}`)
  return (await res.json()).data as { new_count: number; duplicate_count: number; inserted_count: number; error_count: number }
}

async function snap(page: Page, testInfo: TestInfo, name: string) {
  await testInfo.attach(name, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' })
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

async function post(cookie: string, path: string, body: unknown) {
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
