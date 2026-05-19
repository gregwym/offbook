// Mirrors backend/internal/model.Investment.
// Money + quantity arrive as decimal strings (NUMERIC(30,18)). Never
// convert to Number for display — crypto needs the full 18 places.
export type Investment = {
  id: number
  user_id: number
  account_id: number
  ticker: string
  name?: string | null
  asset_class?: string | null
  quantity: string
  cost_basis?: string | null
  market_value?: string | null
  snapshot_date: string // YYYY-MM-DD
  source: 'plaid' | 'csv' | 'manual'
  created_at: string
}

export type CreateInvestmentInput = {
  account_id: number
  ticker: string
  name?: string | null
  asset_class?: string | null
  quantity: string
  cost_basis?: string | null
  market_value?: string | null
  snapshot_date?: string | null // YYYY-MM-DD
  source?: 'plaid' | 'csv' | 'manual'
}

// Mirrors backend service.AssetClassAllocation.
export type AssetClassAllocation = {
  asset_class: string
  market_value: string
  weight_pct: string
}

// Mirrors backend service.RecentChange. Null on the response when no
// holding has two snapshots to compare.
export type RecentChange = {
  delta: string
  holdings_compared: number
  up: number
  down: number
  flat: number
  latest_date: string
  prior_date: string
}

// Mirrors backend service.PortfolioSummary. unrealized_gain_loss is null
// when no holding has both market_value AND cost_basis populated.
export type PortfolioSummary = {
  total_market_value: string
  total_cost_basis: string
  total_unrealized_gain_loss: string | null
  holdings_count: number
  by_asset_class: AssetClassAllocation[]
  recent_change?: RecentChange | null
}
