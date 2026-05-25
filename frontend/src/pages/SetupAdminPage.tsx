import { useState } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuthStore } from '../store/authStore'
import type { SignupMode } from '../types/auth'

export function SetupAdminPage() {
  const { setup, hydrated, user, setupAdmin, error, clearError } = useAuthStore()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [mode, setMode] = useState<SignupMode>('invite_only')
  const [submitting, setSubmitting] = useState(false)

  // Once setup-status reports bootstrapped, this page is no longer reachable.
  if (hydrated && setup?.bootstrapped) {
    return <Navigate to={user ? '/insights' : '/signin'} replace />
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    clearError()
    setSubmitting(true)
    try {
      await setupAdmin(email, password, mode)
      // hydrate-fired Navigate above will move us to /insights.
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell>
      <h1 className="text-xl font-semibold text-gray-900">Set up your admin account</h1>

      <form onSubmit={submit} className="mt-6 space-y-4">
        <Field label="Admin email">
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
            autoFocus
          />
        </Field>
        <Field label="Password (min 8 characters)">
          <input
            type="password"
            required
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
          />
        </Field>
        <Field label="Signup mode">
          <div className="space-y-2 text-sm">
            <ModeOption
              checked={mode === 'invite_only'}
              onChange={() => setMode('invite_only')}
              label="Invite-only (recommended)"
              description="Only the admin can mint invite tokens. Other users sign up by accepting an invite."
            />
            <ModeOption
              checked={mode === 'local_multi_tenant'}
              onChange={() => setMode('local_multi_tenant')}
              label="Local multi-tenant"
              description="Anyone who can reach the instance can create an account. Use only on trusted networks."
            />
          </div>
        </Field>

        {error && (
          <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={submitting || !email || !password}
          className="w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
        >
          {submitting ? 'Creating admin…' : 'Create admin account →'}
        </button>
      </form>
    </AuthShell>
  )
}

export function AuthShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4 py-12">
      <div className="w-full max-w-md rounded-lg border border-gray-200 bg-white p-8 shadow-sm">
        {children}
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="text-sm font-medium text-gray-700 mb-1">{label}</div>
      {children}
    </label>
  )
}

function ModeOption({
  checked,
  onChange,
  label,
  description,
}: {
  checked: boolean
  onChange: () => void
  label: string
  description: string
}) {
  return (
    <label
      className={[
        'flex cursor-pointer items-start gap-2 rounded-md border p-3',
        checked ? 'border-indigo-500 bg-indigo-50' : 'border-gray-200 hover:bg-gray-50',
      ].join(' ')}
    >
      <input type="radio" checked={checked} onChange={onChange} className="mt-0.5" />
      <div>
        <div className="font-medium text-gray-900">{label}</div>
        <div className="text-xs text-gray-500">{description}</div>
      </div>
    </label>
  )
}
