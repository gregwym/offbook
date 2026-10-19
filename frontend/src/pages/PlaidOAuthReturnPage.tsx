// PlaidOAuthReturnPage — /plaid/oauth-return (#502). OAuth institutions
// (Chase, BofA, Wells Fargo, …) finish sign-in on the bank's own site and
// redirect the whole tab back here, which loses Add account's in-memory Link
// state. We re-initialize Link with the link token stashed in sessionStorage
// plus `receivedRedirectUri`, then finish the same chain the originating page
// would have: exchange → sync-accounts → sync-transactions for a new link
// (Add account), or just the syncs for an update-mode re-auth (Settings,
// #364). Every terminal state other than success shows a way back.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { usePlaidLink, type PlaidLinkError } from 'react-plaid-link'
import { Landmark } from 'lucide-react'
import { exchangePublicToken, syncAccounts, syncTransactions } from '../api/plaid'
import {
  clearPendingLink,
  isLinkTokenExpired,
  loadPendingLink,
  savePendingLink,
  type PendingPlaidLink,
} from '../lib/plaidOAuth'

type Step =
  | 'opening_link'
  | 'exchanging'
  | 'syncing_accounts'
  | 'syncing_transactions'
  | 'done'
  | 'missing' // no stored session (new tab, storage cleared, already finished)
  | 'expired' // stored link token expired, or Plaid rejected it
  | 'exited' // user closed Link or Plaid reported an error
  | 'failed' // our exchange/sync call failed

type Initial = { step: Step; pending: PendingPlaidLink | null }

function initialState(): Initial {
  const pending = loadPendingLink()
  if (!pending) return { step: 'missing', pending: null }
  // Link already succeeded before a reload — resume the syncs (idempotent)
  // rather than reopening Link with a consumed token or exchanging twice.
  if (!pending.linkToken) {
    return pending.itemId ? { step: 'syncing_accounts', pending } : { step: 'missing', pending: null }
  }
  if (isLinkTokenExpired(pending)) return { step: 'expired', pending }
  return { step: 'opening_link', pending }
}

export function PlaidOAuthReturnPage() {
  const navigate = useNavigate()
  const [{ step: initialStep, pending }] = useState(initialState)
  const [step, setStep] = useState<Step>(initialStep)
  const [error, setError] = useState<string | null>(null)
  const [itemId, setItemId] = useState<string | null>(pending?.itemId ?? null)
  const mode = pending?.mode ?? 'new'
  const backTo = mode === 'update' ? '/settings' : '/accounts/add'
  const backLabel = mode === 'update' ? 'Back to Settings' : 'Back to Add account'

  // Guards against StrictMode double-effects and a second onSuccess.
  const openedRef = useRef(false)
  const handledRef = useRef(false)
  const resumedRef = useRef(false)

  const runSyncs = useCallback(
    async (id: string) => {
      setError(null)
      try {
        setStep('syncing_accounts')
        await syncAccounts(id)
        setStep('syncing_transactions')
        await syncTransactions(id)
        clearPendingLink()
        setStep('done')
        navigate(mode === 'update' ? '/settings' : '/accounts', { replace: true })
      } catch (e) {
        setError(errMsg(e))
        setStep('failed')
      }
    },
    [mode, navigate],
  )

  const onSuccess = useCallback(
    (publicToken: string) => {
      if (handledRef.current || !pending) return
      handledRef.current = true
      // Drop the token first: a reload from here on must never reopen Link.
      savePendingLink({ ...pending, linkToken: null })
      if (pending.mode === 'update') {
        // Update mode keeps the item's existing access_token; no exchange.
        if (pending.itemId) void runSyncs(pending.itemId)
        return
      }
      void (async () => {
        setStep('exchanging')
        let id: string
        try {
          id = (await exchangePublicToken(publicToken)).item_id
        } catch (e) {
          clearPendingLink()
          setError(errMsg(e))
          setStep('failed')
          return
        }
        savePendingLink({ ...pending, linkToken: null, itemId: id })
        setItemId(id)
        await runSyncs(id)
      })()
    },
    [pending, runSyncs],
  )

  const onExit = useCallback((err: PlaidLinkError | null) => {
    if (handledRef.current) return
    clearPendingLink()
    if (err?.error_code === 'INVALID_LINK_TOKEN') {
      setStep('expired')
      return
    }
    setError(err ? err.display_message || err.error_message || null : null)
    setStep('exited')
  }, [])

  const { open, ready } = usePlaidLink({
    token: step === 'opening_link' ? (pending?.linkToken ?? null) : null,
    receivedRedirectUri: window.location.href,
    onSuccess,
    onExit,
  })

  useEffect(() => {
    if (step === 'opening_link' && ready && !openedRef.current) {
      openedRef.current = true
      open()
    }
  }, [step, ready, open])

  // Reload after a successful Link: pick the import back up.
  useEffect(() => {
    if (initialStep === 'syncing_accounts' && pending?.itemId && !resumedRef.current) {
      resumedRef.current = true
      void runSyncs(pending.itemId)
    }
  }, [initialStep, pending, runSyncs])

  const busy = step === 'opening_link' || step === 'exchanging' || step === 'syncing_accounts' || step === 'syncing_transactions' || step === 'done'

  return (
    <div className="max-w-xl">
      <h1 className="text-2xl font-semibold text-gray-900">
        {mode === 'update' ? 'Reconnecting your bank' : 'Connecting your bank'}
      </h1>
      <div className="mt-6 rounded-lg border border-gray-200 bg-white p-5">
        <div className="flex items-start gap-3">
          <div className="rounded-md bg-indigo-50 p-2 text-indigo-700">
            <Landmark className="h-5 w-5" aria-hidden="true" />
          </div>
          <div className="flex-1">
            {busy ? (
              <div role="status" aria-live="polite" className="flex items-center gap-2 text-sm text-gray-700">
                <span
                  aria-hidden="true"
                  className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-indigo-200 border-t-indigo-600"
                />
                {stepLabel(step)}
              </div>
            ) : (
              <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
                <div role="alert" className="font-medium">
                  {problemTitle(step, itemId)}
                </div>
                <p className="mt-1 whitespace-pre-wrap">{error ?? problemHint(step)}</p>
                <div className="mt-3 flex flex-wrap items-center gap-3">
                  {step === 'failed' && itemId ? (
                    <>
                      <button
                        type="button"
                        onClick={() => void runSyncs(itemId)}
                        className="rounded-md bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
                      >
                        Retry import
                      </button>
                      <Link to="/accounts" className="text-xs font-medium text-red-800 underline">
                        Go to accounts
                      </Link>
                    </>
                  ) : (
                    <Link
                      to={backTo}
                      className="rounded-md bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
                    >
                      {backLabel}
                    </Link>
                  )}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function stepLabel(s: Step): string {
  switch (s) {
    case 'opening_link':
      return 'Finishing bank sign-in with Plaid…'
    case 'exchanging':
      return 'Linking institution…'
    case 'syncing_accounts':
      return 'Loading accounts…'
    case 'syncing_transactions':
      return 'Loading transactions…'
    default:
      return 'Almost done…'
  }
}

function problemTitle(s: Step, itemId: string | null): string {
  switch (s) {
    case 'expired':
      return 'Your bank sign-in session expired'
    case 'missing':
      return 'We couldn’t find a bank sign-in to finish'
    case 'exited':
      return 'Bank sign-in didn’t finish'
    default:
      return itemId ? 'Bank connected, but the import didn’t finish' : 'Couldn’t link your bank'
  }
}

function problemHint(s: Step): string {
  switch (s) {
    case 'expired':
      return 'Plaid sign-in links are only valid for a few hours. Start again to get a fresh one.'
    case 'missing':
      return 'This page only works in the same browser tab you started connecting from. Start again from there.'
    case 'exited':
      return 'Nothing changed. You can start again whenever you’re ready.'
    default:
      return 'Something went wrong.'
  }
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
