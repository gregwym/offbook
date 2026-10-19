// SplitTransactionModal allocates one flow transaction across categories
// (#438). Users type positive amounts; the parent's sign is applied on save,
// so the lines always sum exactly to the transaction. Save is blocked until
// the remaining balance is exactly zero. In a new split the last part tracks
// the remainder (#506) until the user types into it, so a two-way split needs
// one number; saved splits are never rewritten on open.
import { useEffect, useState } from 'react'
import { replaceTransactionSplits } from '../api/transactions'
import type { Category } from '../types/category'
import type { Transaction } from '../types/transaction'
import { absDecimal, addDecimal, subDecimal } from '../lib/decimal'
import { AmountDisplay } from './AmountDisplay'

type Line = { category: string; amount: string }

// asInput pads a computed decimal to at least two places so prefilled parts
// read like typed money ("30.00", not "30"); longer fractions stay exact.
function asInput(d: string): string {
  const [i, f = ''] = d.split('.')
  return f.length >= 2 ? d : `${i}.${f.padEnd(2, '0')}`
}

type Props = {
  tx: Transaction
  currency: string
  categories: Category[]
  onClose: () => void
  onSaved: () => void
}

export function SplitTransactionModal({ tx, currency, categories, onClose, onSaved }: Props) {
  const total = absDecimal(tx.amount)
  const negative = tx.amount.trim().startsWith('-')
  const [lines, setLines] = useState<Line[]>(() =>
    tx.splits && tx.splits.length >= 2
      ? tx.splits.map((s) => ({ category: s.category_id ? String(s.category_id) : '', amount: absDecimal(s.amount) }))
      : [{ category: tx.category_id ? String(tx.category_id) : '', amount: asInput(total) }, { category: '', amount: '' }],
  )
  // Auto-fill the last part only for a fresh split; stops once the user edits
  // the last amount or changes the number of parts.
  const [autoLast, setAutoLast] = useState(!(tx.splits && tx.splits.length >= 2))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const sum = lines.reduce((acc, l) => addDecimal(acc, l.amount.trim() || '0'), '0')
  const remaining = subDecimal(total, sum)
  const balanced = remaining === '0'
  const valid = balanced && lines.length >= 2 && lines.every((l) => /^\d*\.?\d+$/.test(l.amount.trim()) && absDecimal(l.amount) !== '0')
  const incomplete = balanced && !valid
  const label = tx.description ?? tx.merchant_name ?? 'transaction'

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const update = (i: number, patch: Partial<Line>) => {
    const last = lines.length - 1
    if (i === last && patch.amount !== undefined) setAutoLast(false)
    setLines((ls) => {
      const next = ls.map((l, j) => (j === i ? { ...l, ...patch } : l))
      if (!autoLast || i === last || patch.amount === undefined) return next
      const others = next.slice(0, last).reduce((acc, l) => addDecimal(acc, l.amount.trim() || '0'), '0')
      const rest = subDecimal(total, others)
      return next.map((l, j) => (j === last ? { ...l, amount: rest.startsWith('-') || rest === '0' ? '' : asInput(rest) } : l))
    })
  }

  const save = async (clear: boolean) => {
    setError(null)
    setSaving(true)
    try {
      await replaceTransactionSplits(
        tx.id,
        clear
          ? []
          : lines.map((l) => ({
              category_id: l.category ? Number(l.category) : null,
              amount: (negative ? '-' : '') + absDecimal(l.amount),
            })),
      )
      onSaved()
      onClose()
    } catch (err) {
      const r = (err as { response?: { data?: { error?: string } } }).response
      setError(r?.data?.error ?? (err instanceof Error ? err.message : 'Could not save the split. Try again.'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-end justify-center bg-black/40 p-0 sm:items-center sm:p-4" role="dialog" aria-modal="true" aria-label={`Split ${label}`}>
      <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-t-lg bg-white shadow-xl sm:rounded-lg">
        <div className="border-b border-gray-200 px-5 py-3">
          <div className="text-lg font-semibold text-gray-900">Split transaction</div>
          <div className="text-sm text-gray-600">
            {label} · <AmountDisplay amount={tx.amount} currency={currency} signed />
          </div>
        </div>
        <div className="space-y-3 px-5 py-4">
          {tx.splits_need_attention && (
            <p role="alert" className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
              The bank changed this amount, so your earlier split no longer adds up. Until you re-split it,
              reports count the full amount under the transaction&apos;s own category.
            </p>
          )}
          {lines.map((l, i) => (
            <div key={i} className="flex items-center gap-2">
              <select
                aria-label={`Category for part ${i + 1}`}
                value={l.category}
                onChange={(e) => update(i, { category: e.target.value })}
                className="min-h-10 min-w-0 flex-1 rounded border border-gray-300 px-2 text-sm"
              >
                <option value="">Uncategorized</option>
                {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
              <input
                aria-label={`Amount for part ${i + 1}`}
                inputMode="decimal"
                value={l.amount}
                onChange={(e) => update(i, { amount: e.target.value })}
                className="min-h-10 w-28 rounded border border-gray-300 px-2 text-right text-sm"
                placeholder="0.00"
              />
              <button
                type="button"
                aria-label={`Remove part ${i + 1}`}
                disabled={lines.length <= 2}
                onClick={() => { setAutoLast(false); setLines((ls) => ls.filter((_, j) => j !== i)) }}
                className="min-h-10 min-w-10 rounded text-gray-500 hover:bg-gray-100 disabled:opacity-30"
              >
                ×
              </button>
            </div>
          ))}
          <button
            type="button"
            onClick={() => { setAutoLast(false); setLines((ls) => [...ls, { category: '', amount: remaining.startsWith('-') ? '' : remaining === '0' ? '' : asInput(remaining) }]) }}
            disabled={lines.length >= 20}
            className="min-h-9 text-sm text-indigo-700 underline"
          >
            + Add part
          </button>
          <div role="status" className={`rounded px-3 py-2 text-sm ${valid ? 'bg-emerald-50 text-emerald-800' : 'bg-amber-50 text-amber-800'}`}>
            {valid ? (
              'Fully allocated.'
            ) : incomplete ? (
              'Move part of the amount into each line — every part needs a non-zero amount.'
            ) : remaining.startsWith('-') ? (
              <>Over-allocated by <AmountDisplay amount={remaining.slice(1)} currency={currency} />.</>
            ) : (
              <><AmountDisplay amount={remaining} currency={currency} /> left to allocate.</>
            )}
          </div>
          {error && <div role="alert" className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        </div>
        <div className="flex flex-wrap justify-end gap-2 border-t border-gray-200 px-5 py-3">
          {tx.splits && tx.splits.length > 0 && (
            <button type="button" disabled={saving} onClick={() => void save(true)} className="mr-auto min-h-10 text-sm text-red-700 underline">
              Remove split
            </button>
          )}
          <button type="button" onClick={onClose} className="min-h-10 rounded border border-gray-300 px-4 text-sm">Cancel</button>
          <button
            type="button"
            disabled={!valid || saving}
            onClick={() => void save(false)}
            className="min-h-10 rounded bg-indigo-600 px-4 text-sm font-medium text-white disabled:opacity-40"
          >
            {saving ? 'Saving…' : 'Save split'}
          </button>
        </div>
      </div>
    </div>
  )
}
