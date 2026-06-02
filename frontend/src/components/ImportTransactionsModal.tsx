import { useMemo, useState, type ReactNode } from 'react'
import { AlertTriangle, Sparkles, UploadCloud, X } from 'lucide-react'
import { AmountDisplay } from './AmountDisplay'
import { DateDisplay } from './DateDisplay'
import {
  ImportError,
  ImportUnmappableError,
  commitImportJob,
  extractStatement,
  importTransactions,
} from '../api/transactions'
import type { Account } from '../types/account'
import type { ColumnMapping, ImportResult } from '../types/transaction'

type Props = {
  accounts: Account[]
  // Preselect this account when opened from a per-account affordance.
  initialAccountID?: number
  onClose: () => void
  // Called after a successful commit so the caller can refresh its list.
  onImported: (result: ImportResult) => void
}

// The required logical columns, in display order, for manual CSV mapping.
const FIELDS: Array<{ key: keyof ColumnMapping; label: string }> = [
  { key: 'date', label: 'Date' },
  { key: 'amount', label: 'Amount' },
  { key: 'description', label: 'Description' },
]

// fileMode picks the pipeline from the selected file: deterministic CSV vs the
// AI document extractor (PDF/photo). Falls back to CSV for unknown types.
function fileMode(f: File): 'csv' | 'ai' {
  const t = f.type.toLowerCase()
  const name = f.name.toLowerCase()
  if (t === 'application/pdf' || t.startsWith('image/') || name.endsWith('.pdf') || /\.(png|jpe?g|gif|webp)$/.test(name)) {
    return 'ai'
  }
  return 'csv'
}

// ImportTransactionsModal drives statement import. CSV (#330) parses
// deterministically: preview → commit. PDF/photo (ADR-0019) routes through the
// user's AI provider: an explicit-consent "Extract with AI" call stages rows,
// which the user reviews (with per-row confidence + reconciliation) before
// committing. The backend dedups on re-import either way.
export function ImportTransactionsModal({ accounts, initialAccountID, onClose, onImported }: Props) {
  const [accountID, setAccountID] = useState<string>(
    initialAccountID ? String(initialAccountID) : accounts[0] ? String(accounts[0].id) : '',
  )
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<ImportResult | null>(null)
  const [jobID, setJobID] = useState<number | null>(null)
  const [busy, setBusy] = useState<false | 'preview' | 'extract' | 'commit'>(false)
  const [error, setError] = useState<string | null>(null)
  // Acknowledgement gate: when AI extraction flags low-confidence rows, the
  // user must confirm they've reviewed before commit is enabled (ADR-0019 §4).
  const [reviewedAck, setReviewedAck] = useState(false)
  // CSV manual-mapping fallback.
  const [headers, setHeaders] = useState<string[] | null>(null)
  const [mapping, setMapping] = useState<Partial<ColumnMapping>>({})

  const account = useMemo(
    () => accounts.find((a) => String(a.id) === accountID),
    [accounts, accountID],
  )
  const mode = file ? fileMode(file) : 'csv'
  const needsManualMapping = headers !== null
  const mappingComplete = FIELDS.every((f) => (mapping[f.key] ?? '') !== '')

  const resetDerived = () => {
    setPreview(null)
    setJobID(null)
    setHeaders(null)
    setMapping({})
    setReviewedAck(false)
    setError(null)
  }

  const onFileChange = (f: File | null) => {
    setFile(f)
    resetDerived()
  }

  // CSV: deterministic preview / commit.
  const runCSV = async (commit: boolean) => {
    if (!guard()) return
    if (needsManualMapping && !mappingComplete) {
      setError('Map all three columns first.')
      return
    }
    setError(null)
    setBusy(commit ? 'commit' : 'preview')
    try {
      const result = await importTransactions(Number(accountID), file!, {
        commit,
        mapping: needsManualMapping ? mapping : undefined,
      })
      if (commit) {
        onImported(result)
        onClose()
        return
      }
      setPreview(result)
      setHeaders(null)
    } catch (err) {
      if (err instanceof ImportUnmappableError) {
        setHeaders(err.headers)
        setPreview(null)
        setError("Couldn't auto-detect columns — map them below.")
      } else {
        setError(errMsg(err))
      }
    } finally {
      setBusy(false)
    }
  }

  // AI: extract+stage (the egress) → review → commit by job id.
  const runExtract = async () => {
    if (!guard()) return
    setError(null)
    setBusy('extract')
    try {
      const result = await extractStatement(Number(accountID), file!)
      setPreview(result)
      setJobID(result.job_id ?? null)
    } catch (err) {
      handleImportErr(err)
    } finally {
      setBusy(false)
    }
  }

  const runCommitJob = async () => {
    if (jobID == null) return
    setError(null)
    setBusy('commit')
    try {
      const result = await commitImportJob(jobID)
      onImported(result)
      onClose()
    } catch (err) {
      handleImportErr(err)
    } finally {
      setBusy(false)
    }
  }

  const guard = (): boolean => {
    if (!accountID) {
      setError('Pick an account to import into.')
      return false
    }
    if (!file) {
      setError('Choose a file.')
      return false
    }
    return true
  }

  const handleImportErr = (err: unknown) => {
    if (err instanceof ImportError && (err.code === 'AI_IMPORT_UNAVAILABLE')) {
      setError('No AI provider is configured. Add a provider key in Settings to import PDFs or photos.')
      return
    }
    if (err instanceof ImportError && err.code === 'IMPORT_EMPTY_EXTRACTION') {
      setError('No transactions could be read from this file. Try a clearer scan or a CSV export.')
      return
    }
    setError(errMsg(err))
  }

  const reviewBlocksCommit = (preview?.review_count ?? 0) > 0 && !reviewedAck

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-lg bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <span className="text-lg font-semibold text-gray-900">Import transactions</span>
          <button type="button" onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-gray-600">
            <X size={18} />
          </button>
        </div>

        <div className="space-y-4 overflow-y-auto px-5 py-4">
          {error && (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
          )}

          <Field label="Account">
            <select className={inputClass} value={accountID} onChange={(e) => { setAccountID(e.target.value); resetDerived() }}>
              <option value="">(choose)</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </Field>

          <Field label="File">
            <input
              type="file"
              accept=".csv,text/csv,application/pdf,image/png,image/jpeg,image/gif,image/webp"
              className="block w-full text-sm text-gray-700 file:mr-3 file:rounded-md file:border-0 file:bg-indigo-50 file:px-3 file:py-1.5 file:text-sm file:font-medium file:text-indigo-700 hover:file:bg-indigo-100"
              onChange={(e) => onFileChange(e.target.files?.[0] ?? null)}
            />
            <p className="mt-1 text-xs text-gray-500">
              CSV is parsed locally. PDF or photo statements are read by your configured AI provider. Amount: negative =
              money out. Re-importing the same file won't create duplicates.
            </p>
          </Field>

          {/* AI consent notice — shown before the egress, until a preview exists. */}
          {mode === 'ai' && !preview && file && (
            <div className="rounded-md border border-indigo-200 bg-indigo-50 px-3 py-2 text-sm text-indigo-900">
              <p className="font-medium">This file will be sent to your AI provider</p>
              <p className="mt-0.5 text-indigo-800">
                Clicking <span className="font-medium">Extract with AI</span> uploads “{file.name}” to the provider you
                configured in Settings so it can read the transactions. Nothing is saved until you review and import.
              </p>
            </div>
          )}

          {needsManualMapping && headers && (
            <div className="rounded-md border border-amber-200 bg-amber-50 p-3">
              <p className="mb-2 text-sm font-medium text-amber-900">Map your columns</p>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
                {FIELDS.map((f) => (
                  <Field key={f.key} label={f.label}>
                    <select
                      className={inputClass}
                      value={mapping[f.key] ?? ''}
                      onChange={(e) => setMapping((m) => ({ ...m, [f.key]: e.target.value }))}
                    >
                      <option value="">(column)</option>
                      {headers.map((h) => <option key={h} value={h}>{h}</option>)}
                    </select>
                  </Field>
                ))}
              </div>
            </div>
          )}

          {preview && <PreviewSummary result={preview} currency={account?.currency} />}

          {preview && (preview.review_count ?? 0) > 0 && (
            <label className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={reviewedAck}
                onChange={(e) => setReviewedAck(e.target.checked)}
              />
              <span>
                {preview.review_count} row{preview.review_count === 1 ? '' : 's'} were read with low confidence. I’ve
                reviewed the flagged rows above and want to import them.
              </span>
            </label>
          )}
        </div>

        <div className="flex items-center justify-between gap-2 border-t border-gray-200 px-5 py-3">
          <span className="text-xs text-gray-500">
            {preview ? `${preview.total_rows} row${preview.total_rows === 1 ? '' : 's'} read` : ''}
          </span>
          <div className="flex gap-2">
            <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">
              Cancel
            </button>

            {mode === 'csv' ? (
              <>
                <button
                  type="button"
                  onClick={() => runCSV(false)}
                  disabled={busy !== false || !file}
                  className="rounded-md border border-indigo-300 bg-white px-3 py-1.5 text-sm font-medium text-indigo-700 hover:bg-indigo-50 disabled:opacity-50"
                >
                  {busy === 'preview' ? 'Previewing…' : 'Preview'}
                </button>
                <button
                  type="button"
                  onClick={() => runCSV(true)}
                  disabled={busy !== false || !preview || preview.new_count === 0}
                  className="inline-flex items-center gap-1.5 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                >
                  <UploadCloud size={15} />
                  {busy === 'commit' ? 'Importing…' : preview ? `Import ${preview.new_count} new` : 'Import'}
                </button>
              </>
            ) : !preview ? (
              <button
                type="button"
                onClick={runExtract}
                disabled={busy !== false || !file}
                className="inline-flex items-center gap-1.5 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                <Sparkles size={15} />
                {busy === 'extract' ? 'Extracting…' : 'Extract with AI'}
              </button>
            ) : (
              <button
                type="button"
                onClick={runCommitJob}
                disabled={busy !== false || preview.new_count === 0 || reviewBlocksCommit}
                className="inline-flex items-center gap-1.5 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                <UploadCloud size={15} />
                {busy === 'commit' ? 'Importing…' : `Import ${preview.new_count} new`}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function PreviewSummary({ result, currency }: { result: ImportResult; currency?: string }) {
  // Surface low-confidence rows first so the user reviews them (ADR-0019 §4).
  const rows = useMemo(() => {
    return [...result.rows].sort((a, b) => Number(b.needs_review) - Number(a.needs_review))
  }, [result.rows])
  const sample = rows.slice(0, 12)
  const isAI = result.source === 'pdf' || result.source === 'photo'

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2 text-xs">
        <Pill className="border-emerald-300 bg-emerald-50 text-emerald-800">{result.new_count} new</Pill>
        <Pill className="border-gray-300 bg-gray-50 text-gray-600">{result.duplicate_count} duplicate</Pill>
        {result.error_count > 0 && (
          <Pill className="border-red-300 bg-red-50 text-red-700">{result.error_count} error</Pill>
        )}
        {(result.review_count ?? 0) > 0 && (
          <Pill className="border-amber-300 bg-amber-50 text-amber-800">{result.review_count} needs review</Pill>
        )}
      </div>

      {isAI && <ReconcileNote result={result} currency={currency} />}

      <div className="overflow-hidden rounded-md border border-gray-200">
        <table className="w-full text-left text-sm">
          <thead className="bg-gray-50 text-xs uppercase text-gray-500">
            <tr>
              <th className="px-3 py-1.5 font-medium">Date</th>
              <th className="px-3 py-1.5 font-medium">Description</th>
              <th className="px-3 py-1.5 text-right font-medium">Amount</th>
              <th className="px-3 py-1.5 font-medium">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {sample.map((row) => (
              <tr
                key={row.line}
                className={
                  row.status === 'error'
                    ? 'bg-red-50/40'
                    : row.needs_review
                      ? 'bg-amber-50/50'
                      : undefined
                }
              >
                <td className="whitespace-nowrap px-3 py-1.5 text-gray-700">
                  {row.status === 'error' ? <span className="text-gray-400">line {row.line}</span> : <DateDisplay value={row.date} />}
                </td>
                <td className="px-3 py-1.5 text-gray-700">
                  {row.status === 'error' ? <span className="text-red-600">{row.error}</span> : row.description || <span className="text-gray-400">—</span>}
                </td>
                <td className="whitespace-nowrap px-3 py-1.5 text-right">
                  {row.status === 'error' ? '' : <AmountDisplay amount={row.amount} currency={currency} signed />}
                </td>
                <td className="px-3 py-1.5">
                  <div className="flex items-center gap-1.5">
                    <StatusTag status={row.status} />
                    {row.needs_review && row.status !== 'error' && (
                      <span title={`low confidence: ${(row.confidence * 100).toFixed(0)}%`} className="text-amber-600">
                        <AlertTriangle size={13} />
                      </span>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {rows.length > sample.length && (
        <p className="text-xs text-gray-500">Showing first {sample.length} of {rows.length} rows.</p>
      )}
    </div>
  )
}

// ReconcileNote shows whether the extracted row sum matched a statement total —
// a cross-check on AI extraction (ADR-0019 §3).
function ReconcileNote({ result, currency }: { result: ImportResult; currency?: string }) {
  if (result.reconciled == null) {
    return (
      <p className="text-xs text-gray-500">
        No statement total detected to cross-check — review the rows carefully.
      </p>
    )
  }
  if (result.reconciled) {
    return (
      <p className="text-xs text-emerald-700">
        ✓ Row total{result.row_sum ? ' ' : ''}
        {result.row_sum && <AmountDisplay amount={result.row_sum} currency={currency} signed />} matches the statement
        total.
      </p>
    )
  }
  return (
    <p className="flex items-center gap-1.5 text-xs text-amber-700">
      <AlertTriangle size={13} /> Row total
      {result.row_sum && <> <AmountDisplay amount={result.row_sum} currency={currency} signed /> </>}
      didn’t match the statement total{result.doc_totals && result.doc_totals.length > 0 ? ` (${result.doc_totals.join(', ')})` : ''} — review before importing.
    </p>
  )
}

function StatusTag({ status }: { status: ImportResult['rows'][number]['status'] }) {
  const styles: Record<string, string> = {
    new: 'border-emerald-300 bg-emerald-50 text-emerald-800',
    duplicate: 'border-gray-300 bg-gray-50 text-gray-500',
    error: 'border-red-300 bg-red-50 text-red-700',
  }
  return <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${styles[status]}`}>{status}</span>
}

function Pill({ children, className }: { children: ReactNode; className: string }) {
  return <span className={`rounded-full border px-2.5 py-0.5 font-medium ${className}`}>{children}</span>
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
    </label>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function errMsg(err: unknown): string {
  const body = (err as { response?: { data?: { error?: string } } })?.response?.data
  if (body?.error) return body.error
  if (err instanceof Error) return err.message
  return 'Import failed.'
}
