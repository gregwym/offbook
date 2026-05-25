// TradeFormModal records a paired-row trade against a single brokerage
// account. Backend (POST /accounts/:id/trades — service.TradeService)
// validates the rest: the account must be brokerage-type, the asset
// cannot equal the account's cash sleeve, sells cannot exceed holdings.
//
// Asset resolution is symbol+kind based: the user picks from the known
// list or types a new ticker — on submit we ensure (find-or-create) the
// asset and feed the id to the trade endpoint. Fiat assets are filtered
// out of the picker; the cash leg's asset is derived from the account.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { recordTrade } from '../api/trades'
import { useAssetsStore } from '../store/assetsStore'
import type { Account } from '../types/account'
import type { Asset, AssetKind } from '../types/asset'
import type { TradeKind } from '../types/trade'

type Props = {
  account: Account
  onClose: () => void
  // Fires after a successful record so the host page can refetch its data.
  onRecorded?: () => void
}

const TRADEABLE_KINDS: AssetKind[] = ['equity', 'fund', 'crypto', 'bond', 'commodity', 'other']

export function TradeFormModal({ account, onClose, onRecorded }: Props) {
  const { assets, loaded, fetch, ensure } = useAssetsStore()
  const [kind, setKind] = useState<TradeKind>('buy')
  const [symbol, setSymbol] = useState('')
  const [assetKind, setAssetKind] = useState<AssetKind>('equity')
  const [quantity, setQuantity] = useState('')
  const [price, setPrice] = useState('')
  const [tradeDate, setTradeDate] = useState(todayISO())
  const [notes, setNotes] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void fetch()
  }, [fetch])

  const tradeableAssets = useMemo(
    () => assets.filter((a) => TRADEABLE_KINDS.includes(a.kind)),
    [assets],
  )

  // Whenever the user picks/types a symbol, if it matches exactly one
  // known asset, snap the kind dropdown to that asset's kind. Done in
  // the change handler (not an effect) so we don't trigger cascading
  // renders.
  const handleSymbolChange = (next: string) => {
    const up = next.toUpperCase()
    setSymbol(up)
    const sym = up.trim()
    if (!sym) return
    const matches = tradeableAssets.filter((a) => a.symbol === sym)
    if (matches.length === 1) {
      setAssetKind(matches[0].kind)
    }
  }

  const submit = async () => {
    setError(null)
    const sym = symbol.trim().toUpperCase()
    if (!sym) {
      setError('Asset is required.')
      return
    }
    if (!quantity.trim()) {
      setError('Quantity is required.')
      return
    }
    if (!price.trim()) {
      setError('Price is required.')
      return
    }
    if (!tradeDate) {
      setError('Trade date is required.')
      return
    }
    setSubmitting(true)
    try {
      // Find existing first to avoid hammering ensure for known assets.
      let asset: Asset | undefined = tradeableAssets.find(
        (a) => a.symbol === sym && a.kind === assetKind,
      )
      if (!asset) {
        asset = await ensure({ symbol: sym, kind: assetKind })
      }
      await recordTrade(account.id, {
        kind,
        asset_id: asset.id,
        quantity: quantity.trim(),
        price: price.trim(),
        trade_date: tradeDate,
        notes: notes.trim() === '' ? null : notes.trim(),
      })
      onRecorded?.()
      onClose()
    } catch (err) {
      setError(extractErr(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-lg rounded-lg bg-white shadow-xl">
        <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          Record trade — {account.name}
        </div>
        <div className="space-y-3 px-5 py-4">
          {error && (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
              {error}
            </div>
          )}
          <Field label="Side">
            <div className="flex gap-3 text-sm">
              <label className="inline-flex items-center gap-1.5">
                <input type="radio" name="trade-kind" value="buy" checked={kind === 'buy'} onChange={() => setKind('buy')} />
                Buy
              </label>
              <label className="inline-flex items-center gap-1.5">
                <input type="radio" name="trade-kind" value="sell" checked={kind === 'sell'} onChange={() => setKind('sell')} />
                Sell
              </label>
            </div>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Symbol">
              <input
                className={inputClass}
                value={symbol}
                onChange={(e) => handleSymbolChange(e.target.value)}
                placeholder="AAPL"
                list="trade-asset-symbols"
                autoComplete="off"
              />
              <datalist id="trade-asset-symbols">
                {tradeableAssets.map((a) => (
                  <option key={a.id} value={a.symbol}>
                    {a.display_name ? `${a.display_name} · ${a.kind}` : a.kind}
                  </option>
                ))}
              </datalist>
              {!loaded && <p className="mt-1 text-[11px] text-gray-400">Loading known assets…</p>}
            </Field>
            <Field label="Kind">
              <select className={inputClass} value={assetKind} onChange={(e) => setAssetKind(e.target.value as AssetKind)}>
                {TRADEABLE_KINDS.map((k) => (
                  <option key={k} value={k}>{k}</option>
                ))}
              </select>
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Quantity">
              <input
                className={inputClass}
                value={quantity}
                onChange={(e) => setQuantity(e.target.value)}
                inputMode="decimal"
                placeholder="10"
              />
            </Field>
            <Field label={`Price per unit (${account.currency})`}>
              <input
                className={inputClass}
                value={price}
                onChange={(e) => setPrice(e.target.value)}
                inputMode="decimal"
                placeholder="182.00"
              />
            </Field>
          </div>
          <Field label="Trade date">
            <input type="date" className={inputClass} value={tradeDate} onChange={(e) => setTradeDate(e.target.value)} />
          </Field>
          <Field label="Notes (optional)">
            <input className={inputClass} value={notes} onChange={(e) => setNotes(e.target.value)} />
          </Field>
        </div>
        <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? 'Recording…' : 'Record trade'}
          </button>
        </div>
      </div>
    </div>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
    </label>
  )
}

function todayISO(): string {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${dd}`
}

function extractErr(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
