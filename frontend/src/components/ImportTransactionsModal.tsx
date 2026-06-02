import { useMemo, useState, type ReactNode } from 'react'
import { UploadCloud, X } from 'lucide-react'
import { AmountDisplay } from './AmountDisplay'
import { DateDisplay } from './DateDisplay'
import {
  ImportUnmappableError,
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

// The required logical columns, in display order, for manual mapping.
const FIELDS: Array<{ key: keyof ColumnMapping; label: string }> = [
  { key: 'date', label: 'Date' },
  { key: 'amount', label: 'Amount' },
  { key: 'description', label: 'Description' },
]

// ImportTransactionsModal drives the CSV import flow (#330): pick an account,
// upload a file, preview the parsed rows (new / duplicate / error), optionally
// fix column mapping, then commit. The backend dedups on re-import, so a user
// can safely re-upload an overlapping statement.
export function ImportTransactionsModal({ accounts, initialAccountID, onClose, onImported }: Props) {
  const [accountID, setAccountID] = useState<string>(
    initialAccountID ? String(initialAccountID) : accounts[0] ? String(accounts[0].id) : '',
  )
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<ImportResult | null>(null)
  const [busy, setBusy] = useState<false | 'preview' | 'commit'>(false)
  const [error, setError] = useState<string | null>(null)
  // When auto-detection fails, the backend returns the headers; we surface
  // dropdowns so the user can map columns, then re-preview with the override.
  const [headers, setHeaders] = useState<string[] | null>(null)
  const [mapping, setMapping] = useState<Partial<ColumnMapping>>({})

  const account = useMemo(
    () => accounts.find((a) => String(a.id) === accountID),
    [accounts, accountID],
  )

  const needsManualMapping = headers !== null
  const mappingComplete = FIELDS.every((f) => (mapping[f.key] ?? '') !== '')

  const run = async (commit: boolean) => {
    if (!accountID) {
      setError('Pick an account to import into.')
      return
    }
    if (!file) {
      setError('Choose a CSV file.')
      return
    }
    if (needsManualMapping && !mappingComplete) {
      setError('Map all three columns first.')
      return
    }
    setError(null)
    setBusy(commit ? 'commit' : 'preview')
    try {
      const result = await importTransactions(Number(accountID), file, {
        commit,
        mapping: needsManualMapping ? mapping : undefined,
      })
      if (commit) {
        onImported(result)
        onClose()
        return
      }
      setPreview(result)
      setHeaders(null) // mapping succeeded
    } catch (err) {
      if (err instanceof ImportUnmappableError) {
        // Switch to manual mapping mode and let the user assign columns.
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

  // Reset the preview whenever the inputs change, so a stale summary never
  // sits next to a different file/account.
  const onFileChange = (f: File | null) => {
    setFile(f)
    setPreview(null)
    setHeaders(null)
    setMapping({})
    setError(null)
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-lg bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <span className="text-lg font-semibold text-gray-900">Import transactions from CSV</span>
          <button type="button" onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-gray-600">
            <X size={18} />
          </button>
        </div>

        <div className="space-y-4 overflow-y-auto px-5 py-4">
          {error && (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
          )}

          <Field label="Account">
            <select className={inputClass} value={accountID} onChange={(e) => { setAccountID(e.target.value); setPreview(null) }}>
              <option value="">(choose)</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </Field>

          <Field label="CSV file">
            <input
              type="file"
              accept=".csv,text/csv"
              className="block w-full text-sm text-gray-700 file:mr-3 file:rounded-md file:border-0 file:bg-indigo-50 file:px-3 file:py-1.5 file:text-sm file:font-medium file:text-indigo-700 hover:file:bg-indigo-100"
              onChange={(e) => onFileChange(e.target.files?.[0] ?? null)}
            />
            <p className="mt-1 text-xs text-gray-500">
              Expects columns for date, amount, and description. Amount: negative = money out. Re-importing the same
              file won't create duplicates.
            </p>
          </Field>

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
        </div>

        <div className="flex items-center justify-between gap-2 border-t border-gray-200 px-5 py-3">
          <span className="text-xs text-gray-500">
            {preview ? `${preview.total_rows} row${preview.total_rows === 1 ? '' : 's'} parsed` : ''}
          </span>
          <div className="flex gap-2">
            <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">
              Cancel
            </button>
            <button
              type="button"
              onClick={() => run(false)}
              disabled={busy !== false || !file}
              className="rounded-md border border-indigo-300 bg-white px-3 py-1.5 text-sm font-medium text-indigo-700 hover:bg-indigo-50 disabled:opacity-50"
            >
              {busy === 'preview' ? 'Previewing…' : 'Preview'}
            </button>
            <button
              type="button"
              onClick={() => run(true)}
              disabled={busy !== false || !preview || preview.new_count === 0}
              className="inline-flex items-center gap-1.5 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
            >
              <UploadCloud size={15} />
              {busy === 'commit'
                ? 'Importing…'
                : preview
                  ? `Import ${preview.new_count} new`
                  : 'Import'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function PreviewSummary({ result, currency }: { result: ImportResult; currency?: string }) {
  const sample = result.rows.slice(0, 10)
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2 text-xs">
        <Pill className="border-emerald-300 bg-emerald-50 text-emerald-800">{result.new_count} new</Pill>
        <Pill className="border-gray-300 bg-gray-50 text-gray-600">{result.duplicate_count} duplicate</Pill>
        {result.error_count > 0 && (
          <Pill className="border-red-300 bg-red-50 text-red-700">{result.error_count} error</Pill>
        )}
      </div>

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
              <tr key={row.line} className={row.status === 'error' ? 'bg-red-50/40' : undefined}>
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
                  <StatusTag status={row.status} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {result.rows.length > sample.length && (
        <p className="text-xs text-gray-500">Showing first {sample.length} of {result.rows.length} rows.</p>
      )}
    </div>
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
