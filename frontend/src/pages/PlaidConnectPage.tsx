import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePlaidLink } from 'react-plaid-link'
import { Landmark } from 'lucide-react'
import {
  createLinkToken,
  exchangePublicToken,
  syncAccounts,
  syncTransactions,
} from '../api/plaid'

// Steps of the Plaid Link flow, in order. The page tracks the last successful
// step so a retry resumes from where it failed instead of restarting Plaid
// Link (which would burn the user through the institution-selection UI again).
type Step = 'idle' | 'fetching_token' | 'opening_link' | 'exchanging' | 'syncing_accounts' | 'syncing_transactions' | 'done'

type ResumeFrom =
  | { kind: 'token' } // need a fresh link_token + re-open Link
  | { kind: 'exchange'; publicToken: string }
  | { kind: 'sync_accounts'; itemID: string }
  | { kind: 'sync_transactions'; itemID: string }

type Toast = { kind: 'info' | 'success' | 'error'; text: string }

export function PlaidConnectPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>('idle')
  const [error, setError] = useState<string | null>(null)
  const [resume, setResume] = useState<ResumeFrom | null>(null)
  const [toast, setToast] = useState<Toast | null>(null)
  const [linkToken, setLinkToken] = useState<string | null>(null)

  const showToast = useCallback((t: Toast) => {
    setToast(t)
    // Coarse auto-dismiss — long enough to read, short enough not to pile up.
    window.setTimeout(() => setToast((prev) => (prev === t ? null : prev)), 3500)
  }, [])

  const runExchangeAndSync = useCallback(
    async (publicToken: string) => {
      // Step 3: exchange.
      setStep('exchanging')
      setResume({ kind: 'exchange', publicToken })
      let itemID: string
      try {
        const exchanged = await exchangePublicToken(publicToken)
        itemID = exchanged.item_id
        showToast({ kind: 'success', text: `Linked ${exchanged.institution ?? 'institution'}` })
      } catch (e) {
        setError(errMsg(e))
        setStep('idle')
        return
      }

      // Step 4: discover accounts.
      setStep('syncing_accounts')
      setResume({ kind: 'sync_accounts', itemID })
      try {
        const r = await syncAccounts(itemID)
        showToast({ kind: 'success', text: `${r.created} new, ${r.updated} updated accounts` })
      } catch (e) {
        setError(errMsg(e))
        setStep('idle')
        return
      }

      // Step 5: backfill transactions.
      setStep('syncing_transactions')
      setResume({ kind: 'sync_transactions', itemID })
      try {
        const r = await syncTransactions(itemID)
        showToast({ kind: 'success', text: `${r.inserted} new transactions` })
      } catch (e) {
        setError(errMsg(e))
        setStep('idle')
        return
      }

      // Done — hand off to AccountsPage.
      setStep('done')
      setResume(null)
      navigate('/accounts')
    },
    [navigate, showToast],
  )

  const onSuccess = useCallback(
    (publicToken: string) => {
      void runExchangeAndSync(publicToken)
    },
    [runExchangeAndSync],
  )

  const onExit = useCallback(() => {
    // User dismissed Plaid Link without finishing — drop back to idle so the
    // primary button is clickable again. Not a failure; no toast.
    setStep((s) => (s === 'opening_link' ? 'idle' : s))
  }, [])

  const { open, ready } = usePlaidLink({
    token: linkToken,
    onSuccess,
    onExit,
  })

  // Open Plaid Link as soon as we have a token and the SDK is ready. Without
  // this hop, open() called inline after createLinkToken() would fire before
  // ready=true on the first click.
  useEffect(() => {
    if (linkToken && ready && step === 'opening_link') {
      open()
    }
  }, [linkToken, ready, step, open])

  const startFlow = useCallback(async () => {
    setError(null)
    setStep('fetching_token')
    setResume({ kind: 'token' })
    try {
      const t = await createLinkToken()
      setLinkToken(t.link_token)
      setStep('opening_link')
      showToast({ kind: 'info', text: 'Opening Plaid Link…' })
    } catch (e) {
      setError(errMsg(e))
      setStep('idle')
    }
  }, [showToast])

  const retry = useCallback(async () => {
    if (!resume) {
      void startFlow()
      return
    }
    setError(null)
    switch (resume.kind) {
      case 'token':
        void startFlow()
        return
      case 'exchange':
        void runExchangeAndSync(resume.publicToken)
        return
      case 'sync_accounts':
        setStep('syncing_accounts')
        try {
          const r = await syncAccounts(resume.itemID)
          showToast({ kind: 'success', text: `${r.created} new, ${r.updated} updated accounts` })
          setResume({ kind: 'sync_transactions', itemID: resume.itemID })
          setStep('syncing_transactions')
          const t = await syncTransactions(resume.itemID)
          showToast({ kind: 'success', text: `${t.inserted} new transactions` })
          setStep('done')
          setResume(null)
          navigate('/accounts')
        } catch (e) {
          setError(errMsg(e))
          setStep('idle')
        }
        return
      case 'sync_transactions':
        setStep('syncing_transactions')
        try {
          const t = await syncTransactions(resume.itemID)
          showToast({ kind: 'success', text: `${t.inserted} new transactions` })
          setStep('done')
          setResume(null)
          navigate('/accounts')
        } catch (e) {
          setError(errMsg(e))
          setStep('idle')
        }
        return
    }
  }, [navigate, resume, runExchangeAndSync, showToast, startFlow])

  const busy = step !== 'idle' && step !== 'done'
  const buttonLabel = stepLabel(step)

  return (
    <div className="max-w-2xl">
      <h1 className="text-2xl font-semibold text-gray-900">Connect Bank</h1>
      <p className="mt-1 text-sm text-gray-500">
        Link a financial institution via Plaid. Account balances and transaction history will sync into your personal book.
      </p>

      <div className="mt-6 rounded-lg border border-gray-200 bg-white p-6">
        <div className="flex items-start gap-4">
          <div className="rounded-md bg-indigo-50 p-2 text-indigo-700">
            <Landmark size={24} />
          </div>
          <div className="flex-1">
            <div className="text-base font-medium text-gray-900">Link a new institution</div>
            <p className="mt-1 text-sm text-gray-500">
              Plaid will ask you to sign in to your bank securely. Offbook never sees your bank credentials.
            </p>
            {error ? (
              <ErrorPanel message={error} onRetry={retry} resume={resume} />
            ) : (
              <button
                type="button"
                onClick={startFlow}
                disabled={busy}
                className="mt-4 inline-flex items-center gap-2 rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
              >
                {busy && <Spinner />}
                {buttonLabel}
              </button>
            )}
          </div>
        </div>
      </div>

      {toast && (
        <div
          className={[
            'fixed bottom-6 right-6 max-w-sm rounded-md border px-3 py-2 text-sm shadow',
            toast.kind === 'success' && 'border-emerald-200 bg-emerald-50 text-emerald-800',
            toast.kind === 'error' && 'border-red-200 bg-red-50 text-red-800',
            toast.kind === 'info' && 'border-gray-200 bg-white text-gray-800',
          ]
            .filter(Boolean)
            .join(' ')}
          role="status"
        >
          {toast.text}
        </div>
      )}
    </div>
  )
}

function ErrorPanel({
  message,
  onRetry,
  resume,
}: {
  message: string
  onRetry: () => void
  resume: ResumeFrom | null
}) {
  return (
    <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-3 text-sm text-red-800">
      <div className="font-medium">Connection failed</div>
      <div className="mt-1 whitespace-pre-wrap">{message}</div>
      <button
        type="button"
        onClick={onRetry}
        className="mt-3 rounded-md bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700"
      >
        Retry{resume ? ` from ${stageLabel(resume)}` : ''}
      </button>
    </div>
  )
}

function stepLabel(step: Step): string {
  switch (step) {
    case 'idle': return 'Connect Bank'
    case 'fetching_token': return 'Preparing…'
    case 'opening_link': return 'Opening Plaid…'
    case 'exchanging': return 'Linking institution…'
    case 'syncing_accounts': return 'Loading accounts…'
    case 'syncing_transactions': return 'Loading transactions…'
    case 'done': return 'Done'
  }
}

function stageLabel(r: ResumeFrom): string {
  switch (r.kind) {
    case 'token': return 'the start'
    case 'exchange': return 'link exchange'
    case 'sync_accounts': return 'account sync'
    case 'sync_transactions': return 'transaction sync'
  }
}

function Spinner() {
  return (
    <span
      aria-hidden="true"
      className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-white/40 border-t-white"
    />
  )
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) {
      if (r.data.code === 'PLAID_NOT_CONFIGURED') {
        return 'Plaid is not configured on this instance. Ask the admin to set PLAID_CLIENT_ID and PLAID_SECRET.'
      }
      return r.data.error
    }
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
