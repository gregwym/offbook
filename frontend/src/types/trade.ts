// Trade types mirror backend service.RecordTradeInput / TradeRecord
// (see internal/service/trade_service.go). Money + quantity fields are
// decimal strings on the wire — never parse into Number.

import type { Transaction } from './transaction'

export type TradeKind = 'buy' | 'sell'

export type RecordTradeInput = {
  kind: TradeKind
  asset_id: number
  quantity: string  // positive; backend flips sign per kind
  price: string     // per-unit, in account's quote currency
  trade_date: string // YYYY-MM-DD
  notes?: string | null
}

export type Position = {
  id: number
  user_id: number
  account_id: number
  asset_id: number
  quantity: string
  cost_basis?: string | null
  created_at: string
  updated_at: string
}

export type TradeRecord = {
  security_leg: Transaction
  cash_leg: Transaction
  security_position: Position
  cash_position: Position
}
