import { useState, type ReactNode } from 'react'
import type { Category } from '../types/category'
import {
  MATCH_TYPES,
  type CategorizationRule,
  type CreateRuleInput,
  type MatchType,
  type UpdateRuleInput,
} from '../types/categorizationRule'

// Defaults used in 'create' mode. Edit mode ignores defaults and reads
// initial values from the rule itself. Every field is optional so callers
// can pre-fill as much or as little as makes sense (e.g. the transactions-
// table "create rule from this txn" shortcut fills pattern + match_type +
// category_id but leaves the rest at sensible defaults).
export type RuleFormDefaults = {
  pattern?: string
  match_type?: MatchType
  category_id?: number
  priority?: number
  is_active?: boolean
}

type Props = {
  mode: 'create' | 'edit'
  categories: Category[]
  rule?: CategorizationRule
  defaults?: RuleFormDefaults
  onClose: () => void
  onSubmit: (input: CreateRuleInput | UpdateRuleInput) => Promise<void>
}

export function RuleFormModal({ mode, categories, rule, defaults, onClose, onSubmit }: Props) {
  const [pattern, setPattern] = useState(rule?.pattern ?? defaults?.pattern ?? '')
  const [matchType, setMatchType] = useState<MatchType>(
    rule?.match_type ?? defaults?.match_type ?? 'contains',
  )
  const [categoryID, setCategoryID] = useState<number | ''>(
    rule?.category_id ?? defaults?.category_id ?? '',
  )
  const [priority, setPriority] = useState<number>(
    rule?.priority ?? defaults?.priority ?? 10,
  )
  const [isActive, setIsActive] = useState(rule?.is_active ?? defaults?.is_active ?? true)
  const [submitting, setSubmitting] = useState(false)
  // We split form errors by field when the backend gives us a code; otherwise
  // surface as a generic banner above the form.
  const [error, setError] = useState<string | null>(null)
  const [patternError, setPatternError] = useState<string | null>(null)
  const [categoryError, setCategoryError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    setPatternError(null)
    setCategoryError(null)
    if (!pattern.trim()) {
      setPatternError('Pattern is required.')
      return
    }
    if (categoryID === '' || !Number.isInteger(categoryID) || categoryID <= 0) {
      setCategoryError('Pick a category.')
      return
    }
    if (priority < 0) {
      setError('Priority must be >= 0.')
      return
    }
    setSubmitting(true)
    try {
      await onSubmit({
        pattern: pattern.trim(),
        match_type: matchType,
        category_id: Number(categoryID),
        priority,
        is_active: isActive,
      })
    } catch (err) {
      const { code, message } = extractErr(err)
      if (code === 'INVALID_REGEX') {
        setPatternError(message)
      } else if (code === 'UNKNOWN_CATEGORY') {
        setCategoryError(message)
      } else {
        setError(message)
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-lg rounded-lg bg-white shadow-xl">
        <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          {mode === 'create' ? 'New rule' : `Edit "${rule?.pattern}"`}
        </div>
        <div className="space-y-3 px-5 py-4">
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
          <Field label="Pattern" error={patternError}>
            <input
              className={inputClass}
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              placeholder={matchType === 'regex' ? 'e.g. ^AMZN\\s' : 'e.g. WHOLEFDS'}
            />
            <p className="mt-1 text-xs text-gray-500">
              {matchType === 'contains' && 'Case-insensitive substring match on description or merchant.'}
              {matchType === 'exact' && 'Case-insensitive exact match on description or merchant.'}
              {matchType === 'regex' && 'Go regexp; case-sensitive unless you prefix (?i).'}
            </p>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Match type">
              <select className={inputClass} value={matchType} onChange={(e) => setMatchType(e.target.value as MatchType)}>
                {MATCH_TYPES.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </Field>
            <Field label="Priority">
              <input
                className={inputClass}
                type="number"
                min={0}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
              />
            </Field>
          </div>
          <Field label="Category" error={categoryError}>
            <select
              className={inputClass}
              value={categoryID === '' ? '' : String(categoryID)}
              onChange={(e) => setCategoryID(e.target.value === '' ? '' : Number(e.target.value))}
            >
              <option value="">— pick one —</option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </Field>
          <label className="flex items-center gap-2 text-sm text-gray-700">
            <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
            Active
          </label>
        </div>
        <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : mode === 'create' ? 'Create' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function Field({ label, error, children }: { label: string; error?: string | null; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
      {error && <span className="mt-1 block text-xs text-red-600">{error}</span>}
    </label>
  )
}

function extractErr(err: unknown): { code: string | null; message: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) return { code: r.data.code ?? null, message: r.data.error }
  }
  if (err instanceof Error) return { code: null, message: err.message }
  return { code: null, message: 'request failed' }
}
