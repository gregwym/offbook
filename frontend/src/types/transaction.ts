// Transaction mirrors backend model.Transaction. Money fields are decimal
// strings; format via AmountDisplay (never parse to Number).
export type Transaction = {
  id: number
  user_id: number
  account_id: number
  category_id?: number | null
  amount: string
  currency: string
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
  created_at: string
  updated_at: string
}

export const TRANSACTION_SOURCES = ['manual', 'plaid', 'csv', 'pdf'] as const
export type TransactionSource = (typeof TRANSACTION_SOURCES)[number]

// CreateTransactionInput mirrors backend handler.createTransactionRequest.
export type CreateTransactionInput = {
  account_id: number
  category_id?: number | null
  amount: string
  currency?: string
  description?: string | null
  merchant_name?: string | null
  transaction_date: string  // YYYY-MM-DD accepted
  posted_date?: string | null
  source?: TransactionSource
  notes?: string | null
}

// UpdateTransactionInput: sparse patch. `clear_category: true` with a null
// `category_id` uncategorizes; a non-null `category_id` sets it; absent both
// leaves the category alone.
export type UpdateTransactionInput = {
  category_id?: number | null
  clear_category?: boolean
  amount?: string
  currency?: string
  description?: string | null
  merchant_name?: string | null
  transaction_date?: string
  posted_date?: string | null
  notes?: string | null
}
