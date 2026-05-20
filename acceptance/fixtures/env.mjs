import { createHmac, randomBytes } from 'node:crypto'
import { existsSync, readFileSync, appendFileSync } from 'node:fs'
import { resolve } from 'node:path'

const repoRoot = resolve(import.meta.dirname, '../..')

export const personas = [
  { key: 'admin', email: 'qa-admin@offbook.local', role: 'owner' },
  { key: 'contributor', email: 'qa-contributor@offbook.local', role: 'contributor' },
  { key: 'viewer', email: 'qa-viewer@offbook.local', role: 'view_only' },
  { key: 'solo', email: 'qa-solo@offbook.local', role: 'solo' },
]

export function loadQAEnv() {
  for (const name of ['.env.qa', '.env.qa.local']) {
    const path = resolve(repoRoot, name)
    if (!existsSync(path)) continue
    const text = readFileSync(path, 'utf8')
    for (const line of text.split(/\r?\n/)) {
      const trimmed = line.trim()
      if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue
      const [key, ...rest] = trimmed.split('=')
      if (!process.env[key]) process.env[key] = rest.join('=').trim()
    }
  }
}

export function ensureQASecret() {
  loadQAEnv()
  if (process.env.QA_SECRET) return process.env.QA_SECRET

  const secret = randomBytes(32).toString('hex')
  const path = resolve(repoRoot, '.env.qa.local')
  appendFileSync(path, `${existsSync(path) ? '\n' : ''}QA_SECRET=${secret}\n`, { mode: 0o600 })
  process.env.QA_SECRET = secret
  return secret
}

export function personaPassword(email) {
  const secret = ensureQASecret()
  return createHmac('sha256', secret).update(`offbook-qa:${email}`).digest('base64url').slice(0, 28)
}

export function acceptanceAPIURL() {
  return process.env.ACCEPTANCE_API_URL ?? 'http://localhost:18000/api/v1'
}

export function acceptanceBaseURL() {
  return process.env.ACCEPTANCE_BASE_URL ?? 'http://localhost:15173'
}

export function qaDatabaseURL() {
  return process.env.QA_DATABASE_URL ?? 'postgres://offbook:offbook@localhost:15432/offbook_dev?sslmode=disable'
}
