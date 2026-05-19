import { useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { useAuthStore } from '../store/authStore'
import { AuthShell } from './SetupAdminPage'

export function SigninPage() {
  const { setup, hydrated, user, signin, error, clearError } = useAuthStore()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Auth-state-driven redirects: not bootstrapped → setup, already signed-in → dashboard.
  if (hydrated && setup && !setup.bootstrapped) {
    return <Navigate to="/setup/admin" replace />
  }
  if (user) {
    return <Navigate to="/dashboard" replace />
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    clearError()
    setSubmitting(true)
    try {
      await signin(email, password)
    } catch {
      // error surfaces via store; nothing to do.
    } finally {
      setSubmitting(false)
    }
  }

  const canSignup = setup?.signup_mode === 'local_multi_tenant'

  return (
    <AuthShell>
      <h1 className="text-xl font-semibold text-gray-900">Sign in to Offbook</h1>
      <p className="mt-1 text-sm text-gray-500">Your data stays on this instance.</p>

      <form onSubmit={submit} className="mt-6 space-y-4">
        <label className="block">
          <div className="text-sm font-medium text-gray-700 mb-1">Email</div>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoFocus
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
          />
        </label>
        <label className="block">
          <div className="text-sm font-medium text-gray-700 mb-1">Password</div>
          <input
            type="password"
            required
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
          disabled={submitting || !email || !password}
          className="w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
        >
          {submitting ? 'Signing in…' : 'Sign in'}
        </button>
      </form>

      <div className="mt-4 text-center text-xs text-gray-500">
        {canSignup ? (
          <>
            Don't have an account?{' '}
            <Link to="/signup" className="text-indigo-600 hover:text-indigo-700">Sign up</Link>
          </>
        ) : (
          'Signups are invite-only on this instance.'
        )}
      </div>
    </AuthShell>
  )
}
