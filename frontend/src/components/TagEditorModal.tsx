import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import { updateTransaction } from '../api/transactions'
import type { TagCount, Transaction } from '../types/transaction'
import { MAX_TAG_LENGTH, mergeTags } from '../lib/tags'
import { ModalShell } from './ModalShell'

type Props = {
  tx: Transaction
  // The user's existing tags, offered as one-tap suggestions.
  suggestions: TagCount[]
  onClose: () => void
  onSaved: () => void
}

// TagEditorModal edits one transaction's tags (#544). Tags are labels only —
// they never change spending, budgets or balances.
export function TagEditorModal({ tx, suggestions, onClose, onSaved }: Props) {
  const [tags, setTags] = useState<string[]>(tx.tags ?? [])
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const label = tx.description ?? tx.merchant_name ?? 'transaction'

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const has = (t: string) => tags.some((x) => x.toLowerCase() === t.toLowerCase())

  // add applies one or more comma-separated tags; returns the new list, or
  // null after setting an error.
  const add = (raw: string): string[] | null => {
    const { next, error: err } = mergeTags(tags, raw, suggestions)
    setError(err)
    if (next) setTags(next)
    return next
  }

  const save = async () => {
    // A typed-but-not-added tag counts, so "type, then Save" just works.
    const next = draft.trim() === '' ? tags : add(draft)
    if (!next) return
    setDraft('')
    setSaving(true)
    try {
      await updateTransaction(tx.id, { tags: next })
      onSaved()
      onClose()
    } catch (err) {
      const r = (err as { response?: { data?: { error?: string } } }).response
      setError(r?.data?.error ?? 'Could not save tags. Try again.')
    } finally {
      setSaving(false)
    }
  }

  const unused = suggestions.filter((s) => !has(s.name)).slice(0, 12)

  return (
    <ModalShell sheet aria-label={`Tags for ${label}`}>
      <div className="border-b border-gray-200 px-5 py-3">
        <div className="text-lg font-semibold text-gray-900">Tags</div>
        <div className="text-sm text-gray-600">{label}</div>
      </div>
      <div className="space-y-3 px-5 py-4">
        <p className="text-sm text-gray-600">
          Tags group transactions across categories, like <em>vacation</em> or <em>reimbursable</em>.
          They don’t change spending, budgets or balances.
        </p>
        <ul aria-label="Current tags" className="flex flex-wrap gap-2">
          {tags.length === 0 && <li className="text-sm text-gray-500">No tags yet.</li>}
          {tags.map((t) => (
            <li key={t} className="inline-flex items-center gap-1 rounded-full border border-violet-200 bg-violet-50 py-0.5 pl-2.5 pr-1 text-sm text-violet-800">
              {t}
              <button
                type="button"
                aria-label={`Remove tag ${t}`}
                onClick={() => setTags((ts) => ts.filter((x) => x !== t))}
                className="inline-flex min-h-7 min-w-7 items-center justify-center rounded-full hover:bg-violet-100"
              >
                <X size={14} />
              </button>
            </li>
          ))}
        </ul>
        <form
          className="flex gap-2"
          onSubmit={(e) => { e.preventDefault(); if (add(draft)) setDraft('') }}
        >
          <input
            aria-label="New tag"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="Add a tag"
            maxLength={MAX_TAG_LENGTH * 4}
            className="min-h-10 min-w-0 flex-1 rounded border border-gray-300 px-2 text-sm"
          />
          <button type="submit" className="min-h-10 rounded border border-gray-300 px-3 text-sm">Add</button>
        </form>
        {unused.length > 0 && (
          <div>
            <div className="mb-1 text-xs font-medium text-gray-500">Your tags</div>
            <div className="flex flex-wrap gap-2">
              {unused.map((s) => (
                <button
                  key={s.name}
                  type="button"
                  onClick={() => add(s.name)}
                  aria-label={`Add tag ${s.name}`}
                  className="min-h-8 rounded-full border border-gray-300 px-2.5 text-sm text-gray-700 hover:bg-gray-50"
                >
                  + {s.name}
                </button>
              ))}
            </div>
          </div>
        )}
        {error && <div role="alert" className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
      </div>
      <div className="flex flex-wrap justify-end gap-2 border-t border-gray-200 px-5 py-3">
        <button type="button" onClick={onClose} className="min-h-10 rounded border border-gray-300 px-4 text-sm">Cancel</button>
        <button
          type="button"
          disabled={saving}
          onClick={() => void save()}
          className="min-h-10 rounded bg-indigo-600 px-4 text-sm font-medium text-white disabled:opacity-40"
        >
          {saving ? 'Saving…' : 'Save tags'}
        </button>
      </div>
    </ModalShell>
  )
}
