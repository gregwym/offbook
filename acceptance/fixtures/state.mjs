import bcrypt from 'bcryptjs'
import pg from 'pg'
import { randomBytes } from 'node:crypto'
import { acceptanceAPIURL, personaPassword, qaDatabaseURL } from './env.mjs'

const { Client } = pg

export function assertQARole() {
  const cwd = process.cwd().split('/').pop() ?? ''
  if (process.env.OFFBOOK_ROLE === 'qa' || cwd === 'offbook-qa' || cwd.endsWith('-qa')) return
  throw new Error('QA helpers require a QA signal: run from a *-qa worktree or set OFFBOOK_ROLE=qa')
}

export async function withQAClient(fn) {
  const client = new Client({ connectionString: qaDatabaseURL() })
  await client.connect()
  try {
    return await fn(client)
  } finally {
    await client.end()
  }
}

export async function waitForBackend(apiURL = acceptanceAPIURL()) {
  for (let i = 0; i < 30; i += 1) {
    try {
      const res = await fetch(`${apiURL}/health`)
      if (res.ok) return
    } catch {
      // keep polling
    }
    await new Promise((resolve) => setTimeout(resolve, 1000))
  }
  throw new Error(`backend health did not become ready at ${apiURL}/health`)
}

export async function request(path, { method = 'GET', body, cookie } = {}) {
  const res = await fetch(`${acceptanceAPIURL()}${path}`, {
    method,
    headers: {
      'content-type': 'application/json',
      ...(cookie ? { cookie } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
    redirect: 'manual',
  })
  const setCookie = res.headers.get('set-cookie')?.split(';')[0]
  const text = await res.text()
  let json = null
  if (text) json = JSON.parse(text)
  return { res, json, cookie: setCookie }
}

export async function signin(email, password = personaPassword(email)) {
  const out = await request('/auth/signin', {
    method: 'POST',
    body: { email, password },
  })
  if (!out.res.ok) throw new Error(`signin failed for ${email}: HTTP ${out.res.status} ${JSON.stringify(out.json)}`)
  return out.cookie
}

export async function resetToColdStart() {
  assertQARole()
  await withQAClient(async (client) => {
    await client.query('BEGIN')
    try {
      await client.query('TRUNCATE TABLE instance_config RESTART IDENTITY')
      await client.query('TRUNCATE TABLE pii_store, users RESTART IDENTITY CASCADE')
      await client.query('COMMIT')
    } catch (err) {
      await client.query('ROLLBACK')
      throw err
    }
  })
}

export async function createThrowawayUser({ suite, householdID, role = 'contributor' } = {}) {
  assertQARole()
  if (!suite) throw new Error('createThrowawayUser requires a suite name')

  const suffix = `${Date.now()}-${randomBytes(4).toString('hex')}`
  const email = `qa+${suite}-${suffix}@offbook.local`
  const password = personaPassword(email)
  const hash = await bcrypt.hash(password, 12)

  const user = await withQAClient(async (client) => {
    await client.query('BEGIN')
    try {
      const result = await client.query(
        `
          INSERT INTO users (email, password_hash, is_admin, last_scope, default_scope, primary_currency_asset_id, created_at, updated_at)
          VALUES ($1, $2, FALSE, 'personal', 'personal', (SELECT id FROM assets WHERE symbol = 'USD' AND kind = 'fiat'), NOW(), NOW())
          RETURNING id, email
        `,
        [email, hash],
      )
      if (householdID) {
        await client.query(
          `
            INSERT INTO household_members (household_id, user_id, role, joined_at)
            VALUES ($1, $2, $3, NOW())
          `,
          [householdID, result.rows[0].id, role],
        )
        await client.query(
          "UPDATE users SET last_scope = 'household', default_scope = 'household' WHERE id = $1",
          [result.rows[0].id],
        )
      }
      await client.query('COMMIT')
      return result.rows[0]
    } catch (err) {
      await client.query('ROLLBACK')
      throw err
    }
  })

  return { id: Number(user.id), email, password }
}

export async function deleteThrowawayUsers(suite) {
  assertQARole()
  const prefix = `qa+${suite}-%@offbook.local`
  await withQAClient(async (client) => {
    await client.query('BEGIN')
    try {
      const users = await client.query('SELECT id FROM users WHERE email LIKE $1', [prefix])
      if (users.rowCount > 0) {
        const ids = users.rows.map((row) => row.id)
        const accounts = await client.query('SELECT id FROM accounts WHERE user_id = ANY($1::bigint[])', [ids])
        const accountIDs = accounts.rows.map((row) => row.id)
        const households = await client.query(
          "SELECT household_id AS id FROM household_members WHERE user_id = ANY($1::bigint[]) AND role = 'owner'",
          [ids],
        )
        const householdIDs = households.rows.map((row) => row.id)
        await client.query('DELETE FROM sessions WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM plaid_sync_errors WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM plaid_items WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM ai_messages WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM ai_messages WHERE thread_id IN (SELECT id FROM ai_threads WHERE user_id = ANY($1::bigint[]))', [ids])
        await client.query('DELETE FROM ai_threads WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM savings_goals WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM budgets WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM categorization_rules WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM positions WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM account_balance_observations WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM ingestion_jobs WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('UPDATE transactions SET transfer_pair_id = NULL WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM transactions WHERE user_id = ANY($1::bigint[])', [ids])
        if (accountIDs.length > 0) {
          await client.query('DELETE FROM account_shares WHERE account_id = ANY($1::bigint[])', [accountIDs])
          await client.query('DELETE FROM ingestion_jobs WHERE account_id = ANY($1::bigint[])', [accountIDs])
          await client.query('DELETE FROM pii_store WHERE entity_type = $1 AND entity_id = ANY($2::bigint[])', ['account', accountIDs])
        }
        await client.query('DELETE FROM accounts WHERE user_id = ANY($1::bigint[])', [ids])
        if (householdIDs.length > 0) {
          await client.query('DELETE FROM account_shares WHERE household_id = ANY($1::bigint[])', [householdIDs])
          await client.query('DELETE FROM ai_messages WHERE thread_id IN (SELECT id FROM ai_threads WHERE household_id = ANY($1::bigint[]))', [householdIDs])
          await client.query('DELETE FROM ai_threads WHERE household_id = ANY($1::bigint[])', [householdIDs])
          await client.query('DELETE FROM budgets WHERE household_id = ANY($1::bigint[])', [householdIDs])
          await client.query('DELETE FROM savings_goals WHERE household_id = ANY($1::bigint[])', [householdIDs])
          await client.query('DELETE FROM household_invites WHERE household_id = ANY($1::bigint[])', [householdIDs])
          await client.query('DELETE FROM household_members WHERE household_id = ANY($1::bigint[])', [householdIDs])
          await client.query('DELETE FROM households WHERE id = ANY($1::bigint[])', [householdIDs])
        }
        await client.query('DELETE FROM household_invites WHERE created_by = ANY($1::bigint[]) OR accepted_by = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM household_members WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM user_settings WHERE user_id = ANY($1::bigint[])', [ids])
        await client.query('DELETE FROM users WHERE id = ANY($1::bigint[])', [ids])
      }
      await client.query('COMMIT')
    } catch (err) {
      await client.query('ROLLBACK')
      throw err
    }
  })
}
