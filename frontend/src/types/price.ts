// Price mirrors backend model.Price — one observation of
// (asset, quote_asset, as_of, price). Written by providers, trades, and the
// manual "set price" affordance (#373); source identifies which.
export type Price = {
  id: number
  asset_id: number
  quote_asset_id: number
  as_of: string
  price: string
  source: string
  created_at: string
}

// SetManualPriceInput mirrors backend handler.setManualPriceRequest.
// quote_asset_id omitted defaults server-side to the signed-in user's
// primary currency asset — the common case.
export type SetManualPriceInput = {
  quote_asset_id?: number
  price: string
  as_of: string
}
