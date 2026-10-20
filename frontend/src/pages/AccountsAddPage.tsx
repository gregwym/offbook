// AccountsAddPage — v6 §03 "Add account · two tiles, then Plaid does the
// hard part". Single entry point for new accounts (manual + Plaid both
// originate here). Absorbs the old /connect and /import routes. Plaid
// dismissal falls back to the manual form within the same surface.
//
// Statement import is intentionally NOT a tile here — per v6 §03 it lives as a
// per-account "add more" affordance. It's implemented as ImportTransactionsModal
// (#330), reachable from the Transactions page header and per-account on the
// Accounts page. CSV parses locally; PDF/photo route through the user's AI
// provider with explicit consent (ADR-0019).
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { usePlaidLink } from 'react-plaid-link'
import { ArrowLeft, Landmark, PenLine } from 'lucide-react'
import { useAccountsStore } from '../store/accountsStore'
import {
  createLinkToken,
  exchangePublicToken,
  syncAccounts,
  syncTransactions,
} from '../api/plaid'
import { clearPendingLink, savePendingLink } from '../lib/plaidOAuth'
import {
  ACCOUNT_TYPES,
  accountTypeLabel,
  isLiabilityType,
  type AccountType,
  type CreateAccountInput,
} from '../types/account'

type Mode = 'picker' | 'manual'

// Plaid sub-flow state. Mirrors PlaidConnectPage's step machine — we
// keep the same UX (toast on each leg, retry from last successful step)
// but render it inline as part of the picker tile, not as its own route.
type PlaidStep =
  | 'idle'
  | 'fetching_token'
  | 'opening_link'
  | 'exchanging'
  | 'syncing_accounts'
  | 'syncing_transactions'
  | 'done'

export function AccountsAddPage() {
  const navigate = useNavigate()
  const { create } = useAccountsStore()
  const [mode, setMode] = useState<Mode>('picker')

  // Plaid sub-state.
  const [plaidStep, setPlaidStep] = useState<PlaidStep>('idle')
  const [plaidError, setPlaidError] = useState<string | null>(null)
  const [linkToken, setLinkToken] = useState<string | null>(null)
  // After a Plaid dismissal we drop into manual fallback. Tracked so we
  // can show the "Plaid was dismissed — try manually" hint above the form.
  const [plaidDismissed, setPlaidDismissed] = useState(false)

  // Once the token exchange succeeds the bank is linked server-side. A later
  // sync failure must resume the sync for that item — re-opening Link would
  // create a duplicate Plaid item.
  const [linkedItemId, setLinkedItemId] = useState<string | null>(null)

  const runSyncs = useCallback(
    async (itemId: string) => {
      setPlaidError(null)
      try {
        setPlaidStep('syncing_accounts')
        await syncAccounts(itemId)
        setPlaidStep('syncing_transactions')
        await syncTransactions(itemId)
        setPlaidStep('done')
        navigate('/accounts')
      } catch (e) {
        setPlaidError(errMsg(e))
        setPlaidStep('idle')
      }
    },
    [navigate],
  )

  const runExchangeAndSync = useCallback(
    async (publicToken: string) => {
      // Link finished in-page (no OAuth redirect): the stashed resume
      // state for /plaid/oauth-return is no longer needed.
      clearPendingLink()
      setPlaidError(null)
      setPlaidStep('exchanging')
      let itemId: string
      try {
        itemId = (await exchangePublicToken(publicToken)).item_id
      } catch (e) {
        setPlaidError(errMsg(e))
        setPlaidStep('idle')
        return
      }
      setLinkedItemId(itemId)
      await runSyncs(itemId)
    },
    [runSyncs],
  )

  const onSuccess = useCallback(
    (publicToken: string) => {
      void runExchangeAndSync(publicToken)
    },
    [runExchangeAndSync],
  )

  const onExit = useCallback(() => {
    clearPendingLink()
    setPlaidStep((s) => (s === 'opening_link' ? 'idle' : s))
    // Plaid was dismissed without finishing → drop into manual fallback
    // per v6 §03 step 3 ("Plaid dismissed → manual fallback"). Only
    // triggers when the user dismissed mid-Link, not when an earlier
    // step errored — those keep the user on the picker so they can retry.
    setMode('manual')
    setPlaidDismissed(true)
  }, [])

  const { open, ready } = usePlaidLink({ token: linkToken, onSuccess, onExit })

  useEffect(() => {
    if (linkToken && ready && plaidStep === 'opening_link') {
      open()
    }
  }, [linkToken, ready, plaidStep, open])

  const startPlaid = useCallback(async () => {
    setPlaidError(null)
    setPlaidDismissed(false)
    setPlaidStep('fetching_token')
    try {
      const t = await createLinkToken()
      // OAuth banks leave the app and come back to /plaid/oauth-return as a
      // full-page redirect (#502); stash the token so that page can resume.
      savePendingLink({ mode: 'new', linkToken: t.link_token, expiration: t.expiration })
      setLinkToken(t.link_token)
      setPlaidStep('opening_link')
    } catch (e) {
      setPlaidError(errMsg(e))
      setPlaidStep('idle')
    }
  }, [])

  const plaidBusy = plaidStep !== 'idle' && plaidStep !== 'done'

  return (
    <div className="max-w-3xl">
      <div className="flex items-center gap-3">
        <Link
          to="/accounts"
          className="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700"
        >
          <ArrowLeft size={14} /> Back to accounts
        </Link>
      </div>
      <h1 className="mt-2 text-2xl font-semibold text-gray-900">Add an account</h1>
      <p className="mt-1 text-sm text-gray-500">
        Connect a bank via Plaid, or add an account manually. You can switch
        between the two without losing your place.
      </p>

      {mode === 'picker' && (
        <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2">
          <Tile
            icon={<Landmark size={22} />}
            title="Connect a bank"
            description="Plaid handles the institution picker and sign-in. Offbook never sees your bank credentials."
            primary={!plaidError}
            disabled={plaidBusy}
            busyLabel={plaidLabel(plaidStep)}
            onClick={() => void startPlaid()}
            error={plaidError}
            linked={linkedItemId !== null}
            onRetry={
              plaidError
                ? () => void (linkedItemId ? runSyncs(linkedItemId) : startPlaid())
                : undefined
            }
          />
          <Tile
            icon={<PenLine size={22} />}
            title="Add manually"
            description="For accounts Plaid can't reach — crypto wallets, foreign banks, cash, retirement, property."
            onClick={() => setMode('manual')}
          />
        </div>
      )}

      {mode === 'manual' && (
        <ManualAccountForm
          dismissed={plaidDismissed}
          onCreated={async (input) => {
            const account = await create(input)
            // Accounts confirms the new account and offers importing its
            // transactions as the next step (#520).
            navigate('/accounts', { state: { created: { id: account.id, name: account.name } } })
          }}
          onBack={() => {
            setPlaidDismissed(false)
            setMode('picker')
          }}
        />
      )}
    </div>
  )
}

type TileProps = {
  icon: React.ReactNode
  title: string
  description: string
  primary?: boolean
  disabled?: boolean
  busyLabel?: string
  onClick: () => void
  error?: string | null
  onRetry?: () => void
  linked?: boolean
}

function Tile({
  icon,
  title,
  description,
  primary,
  disabled,
  busyLabel,
  onClick,
  error,
  onRetry,
  linked,
}: TileProps) {
  const ring = primary ? 'border-indigo-300 hover:border-indigo-500' : 'border-gray-200 hover:border-gray-400'
  return (
    <div className={`rounded-lg border bg-white p-5 ${ring}`}>
      <div className="flex items-start gap-3">
        <div className="rounded-md bg-indigo-50 p-2 text-indigo-700">{icon}</div>
        <div className="flex-1">
          <div className="text-base font-medium text-gray-900">{title}</div>
          <p className="mt-1 text-sm text-gray-500">{description}</p>
          {error ? (
            <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
              <div role="alert" className="font-medium">
                {linked ? 'Bank connected, but the import didn’t finish' : 'Couldn’t reach Plaid'}
              </div>
              <div className="mt-1 whitespace-pre-wrap">{error}</div>
              <div className="mt-2 flex flex-wrap items-center gap-3">
                {onRetry && (
                  <button
                    type="button"
                    onClick={onRetry}
                    className="rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white hover:bg-red-700"
                  >
                    {linked ? 'Retry import' : 'Retry'}
                  </button>
                )}
                {linked && (
                  <Link to="/accounts" className="text-xs font-medium text-red-800 underline">
                    Go to accounts
                  </Link>
                )}
              </div>
            </div>
          ) : (
            <button
              type="button"
              onClick={onClick}
              disabled={disabled}
              className={[
                'mt-4 inline-flex items-center gap-2 rounded-md px-4 py-2 text-sm font-medium',
                primary
                  ? 'bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-60'
                  : 'border border-gray-300 bg-white text-gray-700 hover:bg-gray-50',
              ].join(' ')}
            >
              {disabled && <Spinner />}
              {disabled ? busyLabel ?? 'Working…' : title}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

function ManualAccountForm({
  dismissed,
  onCreated,
  onBack,
}: {
  dismissed: boolean
  onCreated: (input: CreateAccountInput) => Promise<void>
  onBack: () => void
}) {
  const [name, setName] = useState('')
  const [institution, setInstitution] = useState('')
  const [accountType, setAccountType] = useState<AccountType>('checking')
  const [currency, setCurrency] = useState('USD')
  const [balance, setBalance] = useState('0')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (!name.trim()) {
      setError('Name is required.')
      return
    }
    if (!institution.trim()) {
      setError('Institution is required — the bank or provider, e.g. Ally Bank.')
      return
    }
    if (currency.trim().length !== 3) {
      setError('Currency must be a 3-letter code.')
      return
    }
    const opening = balance.replace(/[,\s$]/g, '') || '0'
    if (!/^-?\d+(\.\d+)?$/.test(opening)) {
      setError('Enter the balance as a number, e.g. 1234.56.')
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      await onCreated({
        name: name.trim(),
        institution_slug: institution.trim(),
        account_type: accountType,
        currency: currency.trim().toUpperCase(),
        balance: opening,
      })
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="mt-6 max-w-lg rounded-lg border border-gray-200 bg-white p-5">
      {dismissed && (
        <div className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
          Plaid was dismissed. You can finish adding the account manually here, or
          <button type="button" onClick={onBack} className="ml-1 underline">
            try Plaid again
          </button>
          .
        </div>
      )}
      <h2 className="text-base font-medium text-gray-900">Add an account manually</h2>
      <p className="mt-1 text-sm text-gray-500">
        Account holder name and full account number live separately in your private PII store — set
        them later from the Accounts page.
      </p>

      <div className="mt-4 space-y-3">
        {error && (
          <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </div>
        )}
        <FormField label="Account name">
          <input
            className={inputClass}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Ally HYSA"
            autoFocus
          />
        </FormField>
        <FormField label="Institution">
          <input
            className={inputClass}
            value={institution}
            onChange={(e) => setInstitution(e.target.value)}
            placeholder="e.g. Ally Bank"
          />
        </FormField>
        <div className="grid grid-cols-2 gap-3">
          <FormField label="Type">
            <select
              className={inputClass}
              value={accountType}
              onChange={(e) => setAccountType(e.target.value as AccountType)}
            >
              {ACCOUNT_TYPES.map((t) => (
                <option key={t} value={t}>
                  {accountTypeLabel(t)}
                </option>
              ))}
            </select>
          </FormField>
          <FormField label="Currency">
            <input
              className={inputClass}
              value={currency}
              maxLength={3}
              onChange={(e) => setCurrency(e.target.value.toUpperCase())}
            />
          </FormField>
        </div>
        <FormField label="Current balance (optional)">
          <input
            className={inputClass}
            value={balance}
            onChange={(e) => setBalance(e.target.value)}
            inputMode="decimal"
            aria-describedby="manual-balance-hint"
          />
        </FormField>
        <p id="manual-balance-hint" className="-mt-2 text-[11px] text-gray-500">
          {isLiabilityType(accountType)
            ? 'Enter what you owe as a negative number, e.g. -450.00.'
            : 'What the bank shows today. You can update it later.'}
        </p>
      </div>

      <div className="mt-5 flex justify-end gap-2">
        <button
          type="button"
          onClick={onBack}
          className="rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700"
        >
          Back
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
        >
          {submitting ? 'Creating…' : 'Create account'}
        </button>
      </div>
    </form>
  )
}

const inputClass =
  'w-full rounded border border-gray-300 px-2 py-1.5 text-sm focus:border-indigo-500 focus:outline-none'

function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
    </label>
  )
}

function Spinner() {
  return (
    <span
      aria-hidden="true"
      className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-white/40 border-t-white"
    />
  )
}

function plaidLabel(s: PlaidStep): string {
  switch (s) {
    case 'idle':
    case 'done':
      return 'Connect a bank'
    case 'fetching_token':
      return 'Preparing…'
    case 'opening_link':
      return 'Opening Plaid…'
    case 'exchanging':
      return 'Linking institution…'
    case 'syncing_accounts':
      return 'Loading accounts…'
    case 'syncing_transactions':
      return 'Loading transactions…'
  }
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
