import { useState } from 'react'
import { Home, Mail, X } from 'lucide-react'
import { acceptInvite, createHousehold } from '../api/households'
import { useScopeStore } from '../store/scopeStore'

type Mode = 'create' | 'join'

// HouseholdJoinModal handles both "create a household" and "accept an
// invite". On success it re-hydrates the scope store so the sidebar swaps
// to household routes immediately, then closes itself.
export function HouseholdJoinModal({ onClose }: { onClose: () => void }) {
  const [mode, setMode] = useState<Mode>('create')
  const [name, setName] = useState('')
  const [token, setToken] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const hydrate = useScopeStore((s) => s.hydrate)

  const submit = async () => {
    setError(null)
    setSubmitting(true)
    try {
      if (mode === 'create') {
        if (!name.trim()) {
          setError('Name is required')
          return
        }
        await createHousehold(name.trim())
      } else {
        if (!token.trim()) {
          setError('Invite token is required')
          return
        }
        await acceptInvite(token.trim())
      }
      // Scope re-hydrate picks up the new membership + flips active to
      // household. The sidebar swaps without a page reload.
      await hydrate()
      onClose()
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <h3 className="text-base font-medium text-gray-900">Join a household</h3>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
          >
            <X size={16} />
          </button>
        </div>

        <div className="flex border-b border-gray-200 text-sm">
          <button
            type="button"
            onClick={() => setMode('create')}
            className={[
              'flex-1 flex items-center justify-center gap-2 py-2 font-medium',
              mode === 'create' ? 'border-b-2 border-indigo-600 text-indigo-700' : 'text-gray-500 hover:text-gray-700',
            ].join(' ')}
          >
            <Home size={14} /> Create
          </button>
          <button
            type="button"
            onClick={() => setMode('join')}
            className={[
              'flex-1 flex items-center justify-center gap-2 py-2 font-medium',
              mode === 'join' ? 'border-b-2 border-indigo-600 text-indigo-700' : 'text-gray-500 hover:text-gray-700',
            ].join(' ')}
          >
            <Mail size={14} /> Accept invite
          </button>
        </div>

        <div className="px-5 py-4 space-y-3">
          {mode === 'create' && (
            <label className="block text-sm">
              <div className="font-medium text-gray-700 mb-1">Household name</div>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Smith Household"
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
                autoFocus
              />
              <p className="mt-1 text-xs text-gray-500">
                You become the owner. You can invite other members afterwards from{' '}
                <code>/h/members</code>.
              </p>
            </label>
          )}

          {mode === 'join' && (
            <label className="block text-sm">
              <div className="font-medium text-gray-700 mb-1">Invite token</div>
              <input
                type="text"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Paste the token your admin shared"
                className="w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs focus:border-indigo-500 focus:outline-none"
                autoFocus
              />
              <p className="mt-1 text-xs text-gray-500">
                Tokens expire after 7 days. If yours is expired, ask the owner to mint a new one.
              </p>
            </label>
          )}

          {error && (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
              {error}
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void submit()}
            disabled={submitting || (mode === 'create' ? !name.trim() : !token.trim())}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
          >
            {submitting ? 'Joining…' : mode === 'create' ? 'Create household' : 'Accept invite'}
          </button>
        </div>
      </div>
    </div>
  )
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
