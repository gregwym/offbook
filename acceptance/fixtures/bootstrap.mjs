#!/usr/bin/env node
import bcrypt from 'bcryptjs'
import pg from 'pg'
import { acceptanceAPIURL, personaPassword, personas, qaDatabaseURL } from './env.mjs'
import { assertQARole, resetToColdStart, signin, waitForBackend } from './state.mjs'

const { Client } = pg
const apiURL = acceptanceAPIURL()

async function upsertUser(client, persona) {
  const hash = await bcrypt.hash(personaPassword(persona.email), 12)
  const isAdmin = persona.key === 'admin'
  const scope = persona.role === 'solo' ? 'personal' : 'household'

  const existing = await client.query(
    'SELECT id FROM users WHERE LOWER(email) = LOWER($1) AND deleted_at IS NULL LIMIT 1',
    [persona.email],
  )
  if (existing.rowCount > 0) {
    const result = await client.query(
      `
        UPDATE users
        SET password_hash = $2,
            is_admin = is_admin OR $3,
            last_scope = $4,
            default_scope = $4,
            updated_at = NOW()
        WHERE id = $1
        RETURNING id
      `,
      [existing.rows[0].id, hash, isAdmin, scope],
    )
    return result.rows[0].id
  }

  const result = await client.query(
    `
      INSERT INTO users (email, password_hash, is_admin, last_scope, default_scope, primary_currency_asset_id, created_at, updated_at)
      VALUES ($1, $2, $3, $4, $4, (SELECT id FROM assets WHERE symbol = 'USD' AND kind = 'fiat'), NOW(), NOW())
      RETURNING id
    `,
    [persona.email, hash, isAdmin, scope],
  )
  return result.rows[0].id
}

async function ensureMembership(client, householdID, userID, role) {
  const existing = await client.query(
    'SELECT id FROM household_members WHERE household_id = $1 AND user_id = $2 AND purged_at IS NULL LIMIT 1',
    [householdID, userID],
  )
  if (existing.rowCount > 0) {
    await client.query(
      'UPDATE household_members SET role = $1, left_at = NULL WHERE id = $2',
      [role, existing.rows[0].id],
    )
    return
  }
  await client.query(
    'INSERT INTO household_members (household_id, user_id, role, joined_at) VALUES ($1, $2, $3, NOW())',
    [householdID, userID, role],
  )
}

async function provisionPersonas() {
  const client = new Client({ connectionString: qaDatabaseURL() })
  await client.connect()
  try {
    await client.query('BEGIN')
    await client.query(
      `
        INSERT INTO instance_config (id, signup_mode, created_at, updated_at)
        VALUES (1, 'invite_only', NOW(), NOW())
        ON CONFLICT (id) DO UPDATE SET signup_mode = 'invite_only', updated_at = NOW()
      `,
    )

    const ids = new Map()
    for (const persona of personas) {
      ids.set(persona.key, await upsertUser(client, persona))
    }

    const adminID = ids.get('admin')
    const existingHousehold = await client.query(
      `
        SELECT h.id FROM households h
        JOIN household_members m ON m.household_id = h.id
          AND m.user_id = $1 AND m.role = 'owner' AND m.purged_at IS NULL
        WHERE h.name = 'QA Household' AND h.deleted_at IS NULL
        ORDER BY h.id LIMIT 1
      `,
      [adminID],
    )
    let householdID = existingHousehold.rows[0]?.id
    if (!householdID) {
      const household = await client.query(
        `
          INSERT INTO households (name, grace_period_days, created_at, updated_at)
          VALUES ('QA Household', 30, NOW(), NOW())
          RETURNING id
        `,
      )
      householdID = household.rows[0]?.id
    }
    if (!householdID) throw new Error('could not create or find QA Household')

    await ensureMembership(client, householdID, adminID, 'owner')
    await ensureMembership(client, householdID, ids.get('contributor'), 'contributor')
    await ensureMembership(client, householdID, ids.get('viewer'), 'view_only')
    await client.query(
      'UPDATE household_members SET left_at = COALESCE(left_at, NOW()) WHERE user_id = $1 AND left_at IS NULL AND purged_at IS NULL',
      [ids.get('solo')],
    )
    await client.query('UPDATE users SET last_scope = $1, default_scope = $1 WHERE id = $2', ['personal', ids.get('solo')])
    await client.query('COMMIT')
  } catch (err) {
    await client.query('ROLLBACK')
    throw err
  } finally {
    await client.end()
  }
}

assertQARole()
await waitForBackend(apiURL)
if (process.argv.includes('--reset')) {
  await resetToColdStart()
}
await provisionPersonas()
for (const persona of personas) {
  await signin(persona.email)
}

console.log(`QA personas ready against ${apiURL}`)
