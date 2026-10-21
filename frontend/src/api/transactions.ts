import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  BulkEditAction,
  BulkEditPrior,
  BulkEditResult,
  ColumnMapping,
  CreateTransactionInput,
  ImportHistoryEntry,
  ImportResult,
  TagCount,
  Transaction,
  UpdateTransactionInput,
} from '../types/transaction'

export type TransactionListFilter = {
  account_id?: number
  // Pass null for uncategorized-only (mapped to ?category_id=null on the wire).
  category_id?: number | null
  // Restricts to rows with this categorization_method. Backed by the v6
  // "Needs review" chip — passing 'plaid_default' shows rows the engine
  // auto-assigned but the user hasn't confirmed.
  categorization_method?: 'plaid_default' | 'rule' | 'manual'
  // false = the review inbox (unreviewed flow rows); true = reviewed rows (#424).
  reviewed?: boolean
  from?: string  // YYYY-MM-DD
  to?: string
  search?: string
  // Exact tag (#544).
  tag?: string
  // Spending lens (#571): only rows the spending figures count; category_id
  // then matches split lines and the response carries spend_total.
  spending?: boolean
  // With spending: the exact Top merchants label.
  merchant?: string
  limit?: number
  offset?: number
}

export async function listTransactions(
  f: TransactionListFilter = {},
): Promise<{ items: Transaction[]; total: number; spend_total?: string }> {
  const params: Record<string, string | number> = {}
  if (f.account_id !== undefined) params.account_id = f.account_id
  if (f.category_id !== undefined) {
    params.category_id = f.category_id === null ? 'null' : f.category_id
  }
  if (f.categorization_method) params.categorization_method = f.categorization_method
  if (f.reviewed !== undefined) params.reviewed = String(f.reviewed)
  if (f.from) params.from = f.from
  if (f.to) params.to = f.to
  if (f.search) params.search = f.search
  if (f.tag) params.tag = f.tag
  if (f.spending) params.spending = 'true'
  if (f.spending && f.merchant) params.merchant = f.merchant
  if (f.limit !== undefined) params.limit = f.limit
  if (f.offset !== undefined) params.offset = f.offset

  const res = await apiClient.get<ApiList<Transaction> & { spend_total?: string }>('/transactions', { params })
  return { items: res.data.data, total: res.data.total, spend_total: res.data.spend_total }
}

// listTransactionTags returns the user's tags with row counts (#544).
export async function listTransactionTags(): Promise<TagCount[]> {
  const res = await apiClient.get<ApiList<TagCount>>('/transactions/tags')
  return res.data.data
}

export async function createTransaction(input: CreateTransactionInput): Promise<Transaction> {
  const res = await apiClient.post<ApiItem<Transaction>>('/transactions', input)
  return res.data.data
}

export async function updateTransaction(id: number, input: UpdateTransactionInput): Promise<Transaction> {
  const res = await apiClient.patch<ApiItem<Transaction>>(`/transactions/${id}`, input)
  return res.data.data
}

// setTransactionsReviewed marks the given rows reviewed (or undoes it) in one
// call; returns how many rows changed (#424).
export async function setTransactionsReviewed(ids: number[], reviewed: boolean): Promise<number> {
  const res = await apiClient.post<ApiItem<{ updated: number }>>('/transactions/review', { ids, reviewed })
  return res.data.data.updated
}

// bulkEditTransactions sets the category of, or hides/unhides, the selected
// rows in one request (#567). The result's undo list reverts exactly this edit.
export async function bulkEditTransactions(ids: number[], action: BulkEditAction): Promise<BulkEditResult> {
  const res = await apiClient.post<ApiItem<BulkEditResult>>('/transactions/bulk-edit', { ids, ...action })
  return res.data.data
}

// undoBulkEdit reverts a bulk edit. Rows edited since are kept, not overwritten.
export async function undoBulkEdit(undo: BulkEditPrior[]): Promise<{ restored: number; kept: number }> {
  const res = await apiClient.post<ApiItem<{ restored: number; kept: number }>>('/transactions/bulk-edit/undo', { undo })
  return res.data.data
}

export type SplitLineInput = { category_id: number | null; amount: string; note?: string }

// replaceTransactionSplits sets (or, with [], clears) a transaction's category
// splits (#438). Amounts carry the parent's sign and must sum to its amount.
export async function replaceTransactionSplits(id: number, splits: SplitLineInput[]): Promise<Transaction> {
  const res = await apiClient.put<ApiItem<Transaction>>(`/transactions/${id}/splits`, { splits })
  return res.data.data
}

export async function deleteTransaction(id: number): Promise<void> {
  await apiClient.delete(`/transactions/${id}`)
}

// ImportUnmappableError is thrown when the backend couldn't auto-detect the
// required columns (code IMPORT_UNMAPPABLE). It carries the source headers and
// the unmatched fields so the UI can render manual column pickers.
export class ImportUnmappableError extends Error {
  headers: string[]
  missing: string[]
  constructor(message: string, headers: string[], missing: string[]) {
    super(message)
    this.name = 'ImportUnmappableError'
    this.headers = headers
    this.missing = missing
  }
}

// ImportError carries the backend machine code (e.g. AI_IMPORT_UNAVAILABLE,
// IMPORT_UNSUPPORTED_TYPE) so the modal can branch — e.g. prompt the user to
// add a provider key in Settings rather than just showing a raw message.
export class ImportError extends Error {
  code: string
  constructor(message: string, code: string) {
    super(message)
    this.name = 'ImportError'
    this.code = code
  }
}

function asImportError(err: unknown): never {
  const body = (err as { response?: { data?: { code?: string; error?: string } } })?.response?.data
  if (body?.code) {
    throw new ImportError(body.error ?? 'Import failed', body.code)
  }
  throw err
}

// listImportHistory returns the account's committed imports, newest first (#376).
export async function listImportHistory(accountID: number): Promise<ImportHistoryEntry[]> {
  const res = await apiClient.get<ApiList<ImportHistoryEntry>>(`/accounts/${accountID}/imports`)
  return res.data.data
}

// undoImport removes the rows a committed import created (IMP-01). The
// returned entry carries undone_at / rows_undone.
export async function undoImport(accountID: number, jobID: number): Promise<ImportHistoryEntry> {
  const res = await apiClient.post<ApiItem<ImportHistoryEntry>>(`/accounts/${accountID}/imports/${jobID}/undo`)
  return res.data.data
}

// extractStatement uploads a PDF or photo (or CSV) to the user's configured AI
// provider for extraction. This is a real egress, so it always sends
// consent=true — callers MUST obtain explicit user consent first. The backend
// extracts once and stages the rows, returning a preview with a job_id; review,
// then call commitImportJob to apply. Nothing is written until commit.
export async function extractStatement(
  accountID: number,
  file: File,
  opts: { allowDuplicate?: boolean } = {},
): Promise<ImportResult> {
  const form = new FormData()
  form.append('file', file)
  form.append('consent', 'true')
  if (opts.allowDuplicate) form.append('allow_duplicate', 'true')
  try {
    const res = await apiClient.post<ApiItem<ImportResult>>(
      `/accounts/${accountID}/transactions/import/extract`,
      form,
      { headers: { 'Content-Type': 'multipart/form-data' } },
    )
    return res.data.data
  } catch (err) {
    return asImportError(err)
  }
}

// commitImportJob applies the rows staged by a prior extractStatement call.
// No second egress — the staged rows are re-validated and inserted server-side.
export async function commitImportJob(jobID: number): Promise<ImportResult> {
  try {
    const res = await apiClient.post<ApiItem<ImportResult>>(
      `/transactions/import/jobs/${jobID}/commit`,
    )
    return res.data.data
  } catch (err) {
    return asImportError(err)
  }
}

// importTransactions uploads a CSV statement into an account. commit=false
// (default) previews — the backend classifies rows and writes nothing; pass
// commit=true to insert the new rows. Optional mapping overrides the
// auto-detected column→field assignment.
export async function importTransactions(
  accountID: number,
  file: File,
  opts: { commit?: boolean; mapping?: Partial<ColumnMapping> } = {},
): Promise<ImportResult> {
  const form = new FormData()
  form.append('file', file)
  form.append('commit', opts.commit ? 'true' : 'false')
  if (opts.mapping?.date) form.append('date_col', opts.mapping.date)
  if (opts.mapping?.amount) form.append('amount_col', opts.mapping.amount)
  if (opts.mapping?.description) form.append('description_col', opts.mapping.description)
  if (opts.mapping?.debit) form.append('debit_col', opts.mapping.debit)
  if (opts.mapping?.credit) form.append('credit_col', opts.mapping.credit)
  if (opts.mapping?.flip_sign) form.append('flip_sign', 'true')

  try {
    const res = await apiClient.post<ApiItem<ImportResult>>(
      `/accounts/${accountID}/transactions/import`,
      form,
      // Let the browser set the multipart boundary; override the client's
      // default application/json.
      { headers: { 'Content-Type': 'multipart/form-data' } },
    )
    return res.data.data
  } catch (err) {
    const body = (err as { response?: { data?: { code?: string; headers?: string[]; missing?: string[]; error?: string } } })
      ?.response?.data
    if (body?.code === 'IMPORT_UNMAPPABLE') {
      throw new ImportUnmappableError(body.error ?? 'Could not detect columns', body.headers ?? [], body.missing ?? [])
    }
    throw err
  }
}
