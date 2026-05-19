import { useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { useAuthStore } from '../store/authStore'
import { AuthShell } from './SetupAdminPage'

export function SignupPage() {
  const { setup, hydrated, user, signup, signupWithInvite, error, clearError } = useAuthStore()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [inviteToken, setInviteToken] = useState('')
  const [submitting, setSubmitting] = useState(false)

  if (hydrated && setup && !setup.bootstrapped) {
    return <Navigate to="/setup/admin" replace />
  }
  if (user) {
    return <Navigate to="/dashboard" replace />
  }

  // Invite-only mode requires a token; local-multi-tenant mode doesn't.
  const inviteRequired = setup?.signup_mode === 'invite_only'

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    clearError()
    setSubmitting(true)
    try {
      if (inviteRequired) {
        await signupWithInvite(email, password, inviteToken)
      } else {
        await signup(email, password)
      }
    } catch {
      // error surfaces via store.
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell>
      <h1 className="text-xl font-semibold text-gray-900">
        {inviteRequired ? 'Accept your invite' : 'Create your account'}
      </h1>
      <p className="mt-1 text-sm text-gray-500">
        {inviteRequired
          ? 'Enter your invite token along with the credentials you want to use. Accepting an invite joins you to the household automatically.'
          : 'Each user gets their own private book. Households are joined by invite later.'}
      </p>

      <form onSubmit={submit} className="mt-6 space-y-4">
        {inviteRequired && (
          <label className="block">
            <div className="text-sm font-medium text-gray-700 mb-1">Invite token</div>
            <input
              type="text"
              required
              value={inviteToken}
              onChange={(e) => setInviteToken(e.target.value)}
              placeholder="Paste the token your admin shared"
              autoFocus
              className="w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs focus:border-indigo-500 focus:outline-none"
            />
            <div className="mt-1 text-[11px] text-gray-500">
              Tokens expire after 7 days. If yours doesn't work, ask the household owner to mint a
              new one.
            </div>
          </label>
        )}
        <label className="block">
          <div className="text-sm font-medium text-gray-700 mb-1">Email</div>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoFocus={!inviteRequired}
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
          />
        </label>
        <label className="block">
          <div className="text-sm font-medium text-gray-700 mb-1">Password (min 8 characters)</div>
          <input
            type="password"
            required
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
          />
        </label>

        {error && (
          <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={submitting || !email || !password || (inviteRequired && !inviteToken.trim())}
          className="w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
        >
          {submitting
            ? inviteRequired
              ? 'Accepting invite…'
              : 'Creating account…'
            : inviteRequired
              ? 'Accept invite & sign up'
              : 'Sign up'}
        </button>
      </form>

      <div className="mt-4 text-center text-xs text-gray-500">
        Already have an account?{' '}
        <Link to="/signin" className="text-indigo-600 hover:text-indigo-700">Sign in</Link>
      </div>
    </AuthShell>
  )
}
