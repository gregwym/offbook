import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  ColumnMapping,
  CreateTransactionInput,
  ImportResult,
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
  from?: string  // YYYY-MM-DD
  to?: string
  search?: string
  limit?: number
  offset?: number
}

export async function listTransactions(
  f: TransactionListFilter = {},
): Promise<{ items: Transaction[]; total: number }> {
  const params: Record<string, string | number> = {}
  if (f.account_id !== undefined) params.account_id = f.account_id
  if (f.category_id !== undefined) {
    params.category_id = f.category_id === null ? 'null' : f.category_id
  }
  if (f.categorization_method) params.categorization_method = f.categorization_method
  if (f.from) params.from = f.from
  if (f.to) params.to = f.to
  if (f.search) params.search = f.search
  if (f.limit !== undefined) params.limit = f.limit
  if (f.offset !== undefined) params.offset = f.offset

  const res = await apiClient.get<ApiList<Transaction>>('/transactions', { params })
  return { items: res.data.data, total: res.data.total }
}

export async function createTransaction(input: CreateTransactionInput): Promise<Transaction> {
  const res = await apiClient.post<ApiItem<Transaction>>('/transactions', input)
  return res.data.data
}

export async function updateTransaction(id: number, input: UpdateTransactionInput): Promise<Transaction> {
  const res = await apiClient.patch<ApiItem<Transaction>>(`/transactions/${id}`, input)
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

// extractStatement uploads a PDF or photo (or CSV) to the user's configured AI
// provider for extraction. This is a real egress, so it always sends
// consent=true — callers MUST obtain explicit user consent first. The backend
// extracts once and stages the rows, returning a preview with a job_id; review,
// then call commitImportJob to apply. Nothing is written until commit.
export async function extractStatement(accountID: number, file: File): Promise<ImportResult> {
  const form = new FormData()
  form.append('file', file)
  form.append('consent', 'true')
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
