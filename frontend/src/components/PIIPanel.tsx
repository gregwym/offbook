import { useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { getAccountPII, updateAccountPII } from '../api/accounts'
import { PII_FIELDS, type AccountPII, type AccountPIIField } from '../types/account'

type Props = {
  accountID: number
}

const FIELD_LABELS: Record<AccountPIIField, string> = {
  holder_name: 'Holder name',
  account_number: 'Account number',
  routing_number: 'Routing number',
  address: 'Address',
}

// PIIPanel is a collapsible inside the Edit modal. PII is fetched ONLY when
// the panel is expanded — verifiable in DevTools network: open the modal,
// observe NO /pii call until the user clicks "Show". This matches the
// architectural promise that PII access is deliberate and auditable.
export function PIIPanel({ accountID }: Props) {
  const [expanded, setExpanded] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reveal, setReveal] = useState<Record<AccountPIIField, boolean>>({
    holder_name: false, account_number: false, routing_number: false, address: false,
  })
  const [values, setValues] = useState<AccountPII>({})
  const [saving, setSaving] = useState(false)

  const toggle = async () => {
    const next = !expanded
    setExpanded(next)
    if (next && !loaded) {
      setLoading(true)
      setError(null)
      try {
        const pii = await getAccountPII(accountID)
        setValues(pii)
        setLoaded(true)
      } catch (err) {
        setError(errMsg(err))
      } finally {
        setLoading(false)
      }
    }
  }

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      // Filter out empty strings — the backend rejects empty values per
      // ErrEmptyPIIValue. Users clear a field by leaving it blank, which we
      // simply omit from the PUT.
      const payload: AccountPII = {}
      for (const f of PII_FIELDS) {
        const v = values[f]
        if (v && v.trim() !== '') payload[f] = v.trim()
      }
      const fresh = await updateAccountPII(accountID, payload)
      setValues(fresh)
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="rounded-md border border-gray-200">
      <button
        type="button"
        onClick={toggle}
        className="flex w-full items-center justify-between px-4 py-3 text-sm font-medium text-gray-700 hover:bg-gray-50"
      >
        <span>PII (holder, account number, routing, address)</span>
        <span className="text-xs text-gray-500">{expanded ? 'Hide' : 'Show'}</span>
      </button>
      {expanded && (
        <div className="space-y-3 border-t border-gray-200 px-4 py-3">
          {loading && <div className="text-sm text-gray-500">Loading…</div>}
          {error && <div className="text-sm text-red-700">{error}</div>}
          {!loading && (
            <>
              {PII_FIELDS.map((field) => (
                <div key={field}>
                  <label className="mb-1 block text-xs font-medium text-gray-600">
                    {FIELD_LABELS[field]}
                  </label>
                  <div className="flex items-stretch gap-2">
                    <input
                      type={reveal[field] ? 'text' : 'password'}
                      value={values[field] ?? ''}
                      onChange={(e) => setValues({ ...values, [field]: e.target.value })}
                      autoComplete="off"
                      className="flex-1 rounded border border-gray-300 px-2 py-1 text-sm"
                    />
                    <button
                      type="button"
                      onClick={() => setReveal({ ...reveal, [field]: !reveal[field] })}
                      className="rounded border border-gray-300 px-2 text-gray-600 hover:bg-gray-100"
                      aria-label={reveal[field] ? 'Hide' : 'Reveal'}
                    >
                      {reveal[field] ? <EyeOff size={14} /> : <Eye size={14} />}
                    </button>
                  </div>
                </div>
              ))}
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={save}
                  disabled={saving}
                  className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                >
                  {saving ? 'Saving…' : 'Save PII'}
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
