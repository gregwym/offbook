// Transaction mirrors backend model.Transaction. Money fields are decimal
// strings; format via AmountDisplay (never parse to Number). Per ADR-0013,
// the transaction's unit is `asset_id` — for cash legs it equals the
// parent account's primary_quote_asset; for trade security legs it's the
// security (AAPL, BTC, …).
export type TransactionSplit = {
  id: number
  transaction_id: number
  category_id?: number | null
  amount: string
  note?: string | null
}

export type Transaction = {
  id: number
  user_id: number
  account_id: number
  asset_id: number
  category_id?: number | null
  // kind classifies the row (ADR-0017). 'flow' is ordinary cash movement;
  // 'opening_balance'/'adjustment' are reconciliation rows (#370) — quantity
  // facts excluded from spending/budget analytics. 'trade_leg' is one side
  // of a paired trade.
  kind: TransactionKind
  amount: string
  description?: string | null
  description_clean?: string | null
  merchant_name?: string | null
  transaction_date: string  // YYYY-MM-DD (DATE column)
  posted_date?: string | null
  source: TransactionSource
  external_id?: string | null
  plaid_transaction_id?: string | null
  categorization_method?: string | null
  is_transfer: boolean
  transfer_pair_id?: number | null
  notes?: string | null
  // caused_by_observation_id/acknowledged_* are reconciliation metadata
  // (#370), meaningful only on kind='adjustment'/'opening_balance' rows.
  caused_by_observation_id?: number | null
  acknowledged_at?: string | null
  acknowledged_note?: string | null
  // reviewed_at is explicit user review state (#424), independent of
  // categorization: null/absent = unreviewed (in the review inbox).
  reviewed_at?: string | null
  // hidden_at (#465): the user excluded this flow row from spending, income,
  // budgets and merchant reports. Still in the list; balances are unaffected.
  hidden_at?: string | null
  // splits (#438): category allocation lines summing to amount; absent = unsplit.
  splits?: TransactionSplit[]
  // splits_need_attention (#445): the bank revised the amount so splits no
  // longer sum to it; reporting uses the parent row until re-split.
  splits_need_attention?: boolean
  // pending (#513): the bank hasn't posted this yet. The user's edits carry
  // over when it posts (the posted row adopts this one).
  pending?: boolean
  created_at: string
  updated_at: string
}

export const TRANSACTION_SOURCES = ['manual', 'plaid', 'csv', 'pdf'] as const
export type TransactionSource = (typeof TRANSACTION_SOURCES)[number]

export const TRANSACTION_KINDS = ['flow', 'trade_leg', 'opening_balance', 'adjustment'] as const
export type TransactionKind = (typeof TRANSACTION_KINDS)[number]

// CreateTransactionInput mirrors backend handler.createTransactionRequest.
// Per ADR-0013, no `currency` field — the asset is derived server-side
// from the parent account.
export type CreateTransactionInput = {
  account_id: number
  category_id?: number | null
  amount: string
  description?: string | null
  merchant_name?: string | null
  transaction_date: string  // YYYY-MM-DD accepted
  posted_date?: string | null
  source?: TransactionSource
  notes?: string | null
}

// ── CSV import (#330) — mirrors backend service.ImportResult ──

// ColumnMapping ties the three logical fields to source-CSV header names.
// ColumnMapping mirrors ingestion.ColumnMapping. Money comes from either one
// signed `amount` column or a `debit` (money out) + `credit` (money in) pair;
// flip_sign negates every amount, for exports that list purchases as positive
// (#542). The backend echoes the resolved mapping on every preview.
export type ColumnMapping = {
  date: string
  amount: string
  debit?: string
  credit?: string
  description: string
  flip_sign?: boolean
}

export type ImportRowStatus = 'new' | 'duplicate' | 'error'

export type ImportRowResult = {
  line: number
  date: string
  amount: string
  description: string
  external_id?: string
  status: ImportRowStatus
  error?: string
  // confidence is the extractor's certainty (0–1). Deterministic CSV rows are
  // 1.0; the AI extractor (ADR-0019 phase 2) reports real values. needs_review
  // flags low-confidence new rows the UI surfaces before commit.
  confidence: number
  needs_review: boolean
}

export type ImportResult = {
  committed: boolean
  source: string
  mapping: ColumnMapping
  headers: string[]
  total_rows: number
  new_count: number
  duplicate_count: number
  error_count: number
  review_count: number
  inserted_count: number
  rows: ImportRowResult[]
  // AI-extraction fields (ADR-0019), absent for deterministic CSV:
  //   job_id     — the staged import job; pass to commitImportJob to apply.
  //   reconciled — null when no statement total was detected; else whether the
  //                row sum matched one.
  //   row_sum    — signed decimal sum of valid rows.
  //   doc_totals — statement totals the extractor detected.
  job_id?: number
  reconciled?: boolean | null
  row_sum?: string
  doc_totals?: string[]
  // duplicate_of is set when byte-identical content was already committed into
  // this account (#376). Informational — row-level dedup still applies.
  duplicate_of?: PriorImport
  positions?: PositionPreview[]
  closing_balance?: BalancePreview
  positions_reconciled?: number
  adjustments_written?: number
}

// ── Statement positions + closing balance (#375) — mirrors backend service ──

export type ReconcileEffect = {
  current_quantity: string
  // reported − current_quantity; "0" means no ledger row is written.
  adjustment: string
  kind: 'opening_balance' | 'adjustment' | ''
}

export type PositionPreview = {
  line: number
  symbol: string
  name?: string
  asset_kind: string
  quantity: string
  price?: string
  value?: string
  as_of: string
  confidence: number
  needs_review: boolean
  error?: string
  // 'new' = the asset is created on commit; shown so it is never silent.
  asset_status?: 'existing' | 'new'
  effect?: ReconcileEffect
}

export type BalancePreview = {
  symbol: string
  reported: string
  as_of: string
  effect: ReconcileEffect
}

export type PriorImport = {
  job_id: number
  file_name?: string
  imported_at?: string
}

// ImportHistoryEntry mirrors backend service.ImportHistoryEntry (#376).
export type ImportHistoryEntry = {
  job_id: number
  source: string
  extractor?: string
  file_name?: string
  imported_at: string
  rows_total: number
  rows_imported: number
  rows_duplicate: number
  rows_error: number
  row_sum?: string
  doc_totals?: string[]
  // reconciled is null when the document had no statement total to compare;
  // difference is the gap to the nearest total when reconciled is false.
  reconciled: boolean | null
  difference?: string
  // Undo (IMP-01): undone_at is set once the import's rows were removed;
  // can_undo is false for undone or legacy imports with no linked rows, and
  // for an older import while undo_blocked_by (a newer import that recorded
  // the account's balance) still stands — undo that one first.
  undone_at?: string
  rows_undone?: number
  can_undo: boolean
  undo_blocked_by?: number
}

// UpdateTransactionInput: sparse patch. `clear_category: true` with a null
// `category_id` uncategorizes; a non-null `category_id` sets it; absent both
// leaves the category alone.
export type UpdateTransactionInput = {
  category_id?: number | null
  clear_category?: boolean
  amount?: string
  description?: string | null
  merchant_name?: string | null
  transaction_date?: string
  posted_date?: string | null
  notes?: string | null
  // true marks reviewed, false undoes it; independent of category edits (#424).
  reviewed?: boolean
  // true hides the row from spending/budget reports, false unhides it (#465).
  hidden?: boolean
}
