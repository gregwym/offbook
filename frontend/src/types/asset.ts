// Asset mirrors backend model.Asset. Reference data shared across the
// instance (USD, AAPL, BTC) — not user-scoped. The trade form uses these
// to resolve an asset_id for POST /accounts/:id/trades.

export const ASSET_KINDS = [
  'fiat',
  'equity',
  'fund',
  'crypto',
  'bond',
  'commodity',
  'other',
] as const

export type AssetKind = (typeof ASSET_KINDS)[number]

// Plain-language names for asset classes (#524). The backend codes ('fiat',
// 'equity', …) are storage values, not copy.
const ASSET_KIND_LABELS: Record<AssetKind, string> = {
  fiat: 'Cash',
  equity: 'Stocks',
  fund: 'Funds',
  crypto: 'Crypto',
  bond: 'Bonds',
  commodity: 'Commodities',
  other: 'Other',
}

export function assetKindLabel(kind: string): string {
  return (
    ASSET_KIND_LABELS[kind as AssetKind] ?? (kind ? kind.charAt(0).toUpperCase() + kind.slice(1) : 'Other')
  )
}

export type Asset = {
  id: number
  symbol: string
  kind: AssetKind
  display_name?: string | null
  quote_currency_asset_id?: number | null
  precision: number
  created_at: string
  updated_at: string
}

// EnsureAssetInput mirrors backend handler.ensureAssetRequest. Backend
// uppercases symbol and falls back display_name → symbol when blank.
export type EnsureAssetInput = {
  symbol: string
  kind: AssetKind
  display_name?: string
}
