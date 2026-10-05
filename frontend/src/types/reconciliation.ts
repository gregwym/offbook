// Mirrors backend service.ReconciliationCheckpoint / ReconciliationReport
// (#370). Money/quantity fields are decimal strings — format via
// AmountDisplay, never parse to Number.
export type ReconciliationCheckpoint = {
  observation_id: number
  asset_id: number
  as_of: string
  source: string
  observed_quantity: string
  prior_fold: string
  delta: string
  // transaction_id/kind are absent when delta is "0" — the observation
  // matched the ledger fold and no reconciling row was written.
  transaction_id?: number | null
  kind?: string
  acknowledged: boolean
  acknowledged_at?: string | null
  acknowledged_note?: string | null
  needs_attention: boolean
}

export type ReconciliationReport = {
  account_id: number
  checkpoints: ReconciliationCheckpoint[]
  needs_attention: boolean
}
