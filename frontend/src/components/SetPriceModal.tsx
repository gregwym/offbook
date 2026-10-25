// SetPriceModal is the Tier-1 manual price-entry affordance (#373,
// ADR-0013 §5): the always-available pricing floor for an asset no
// provider covers and no trade has priced yet. Surfaced inline wherever a
// partial/stale valuation is flagged (Insights net worth + allocation,
// Accounts balance). Asset resolution mirrors TradeFormModal — pick a
// known symbol or type a new one; on submit we ensure (find-or-create)
// the asset and post the price against it.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { setManualPrice } from '../api/prices'
import { useAssetsStore } from '../store/assetsStore'
import { assetKindLabel, type Asset, type AssetKind } from '../types/asset'
import { ModalShell } from './ModalShell'

type Props = {
  // initial pre-fills the asset when the caller already knows which holding
  // needs a price (#530) — the user then only types the price.
  // A fiat initial opens the modal as an exchange-rate entry (#599): the
  // currency is fixed and the value is what one unit is worth in quote (the
  // primary currency the net-worth headline is in).
  initial?: { symbol: string; kind: string; quote?: string }
  onClose: () => void
  // Fires after a successful set so the host page can refetch its data.
  onSet?: () => void
}

// Fiat-to-fiat rates normally come from the FX provider seam, so the picker
// is scoped to priceable, non-fiat holdings (equities, funds, crypto, bonds,
// commodities, or an uncategorized ticker). A currency the provider doesn't
// cover is reachable only through a fiat `initial` (#599).
const PRICEABLE_KINDS: AssetKind[] = ['equity', 'fund', 'crypto', 'bond', 'commodity', 'other']

export function SetPriceModal({ initial, onClose, onSet }: Props) {
  const { assets, loaded, fetch, ensure } = useAssetsStore()
  const rateFor = initial?.kind === 'fiat' ? initial.symbol : null
  const rateMode = rateFor !== null
  const quote = initial?.quote ?? 'your primary currency'
  const [symbol, setSymbol] = useState(initial?.symbol ?? '')
  const [assetKind, setAssetKind] = useState<AssetKind>(
    initial && PRICEABLE_KINDS.includes(initial.kind as AssetKind) ? (initial.kind as AssetKind) : 'equity',
  )
  const [price, setPrice] = useState('')
  const [asOf, setAsOf] = useState(todayISO())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void fetch()
  }, [fetch])

  const priceableAssets = useMemo(
    () => assets.filter((a) => PRICEABLE_KINDS.includes(a.kind)),
    [assets],
  )

  const handleSymbolChange = (next: string) => {
    const up = next.toUpperCase()
    setSymbol(up)
    const matches = priceableAssets.filter((a) => a.symbol === up.trim())
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
    if (!price.trim()) {
      setError(rateMode ? 'Rate is required.' : 'Price is required.')
      return
    }
    if (!asOf) {
      setError('Date is required.')
      return
    }
    setSubmitting(true)
    try {
      const kind: AssetKind = rateMode ? 'fiat' : assetKind
      let asset: Asset | undefined = assets.find((a) => a.symbol === sym && a.kind === kind)
      if (!asset) {
        asset = await ensure({ symbol: sym, kind })
      }
      await setManualPrice(asset.id, { price: price.trim(), as_of: asOf })
      onSet?.()
      onClose()
    } catch (err) {
      setError(extractErr(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <ModalShell size="md" aria-labelledby="set-price-title">
      <div id="set-price-title" className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
        {rateMode
          ? `Set exchange rate for ${rateFor}`
          : initial
            ? `Set price for ${initial.symbol}`
            : 'Set price'}
      </div>
      <div className="space-y-3 px-5 py-4">
        <p className="text-xs text-gray-500">
          {rateMode
            ? `${rateFor} has no exchange rate from the last 7 days, so net worth leaves your ${rateFor} balances out. Enter what 1 ${rateFor} is worth in ${quote} today.`
            : initial
              ? `${initial.symbol} has no price from the last 7 days, so balances leave it out. Enter what one unit is worth today.`
              : 'For an asset no price provider covers — a private fund, obscure ticker, or collectible. Priced in your primary currency.'}
        </p>
        {error && (
          <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </div>
        )}
        {!rateMode && (
        <div className="grid grid-cols-2 gap-3">
          <Field label="Symbol">
            <input
              className={inputClass}
              value={symbol}
              onChange={(e) => handleSymbolChange(e.target.value)}
              placeholder="e.g. MYFUND"
              list="set-price-asset-symbols"
              autoComplete="off"
            />
            <datalist id="set-price-asset-symbols">
              {priceableAssets.map((a) => (
                <option key={a.id} value={a.symbol}>
                  {a.display_name ? `${a.display_name} · ${assetKindLabel(a.kind)}` : assetKindLabel(a.kind)}
                </option>
              ))}
            </datalist>
            {!loaded && <p className="mt-1 text-[11px] text-gray-400">Loading known assets…</p>}
          </Field>
          <Field label="Kind">
            <select
              className={inputClass}
              value={assetKind}
              onChange={(e) => setAssetKind(e.target.value as AssetKind)}
            >
              {PRICEABLE_KINDS.map((k) => (
                <option key={k} value={k}>
                  {assetKindLabel(k)}
                </option>
              ))}
            </select>
          </Field>
        </div>
        )}
        <Field label={rateMode ? `1 ${rateFor} in ${quote}` : 'Price (per unit)'}>
          <input
            className={inputClass}
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            inputMode="decimal"
            placeholder={rateMode ? '1.08' : '100.00'}
            autoFocus={initial != null}
          />
        </Field>
        <Field label="As of">
          <input type="date" className={inputClass} value={asOf} onChange={(e) => setAsOf(e.target.value)} />
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
          {submitting ? 'Saving…' : rateMode ? 'Set rate' : 'Set price'}
        </button>
      </div>
    </ModalShell>
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
