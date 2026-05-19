import { useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { Lock } from 'lucide-react'
import { useAuthStore } from '../store/authStore'
import { AuthShell } from './SetupAdminPage'

export function SignupPage() {
  const { setup, hydrated, user, signup, error, clearError } = useAuthStore()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)

  if (hydrated && setup && !setup.bootstrapped) {
    return <Navigate to="/setup/admin" replace />
  }
  if (user) {
    return <Navigate to="/dashboard" replace />
  }

  // invite_only mode: signup endpoint is closed for new users. Backend
  // support for signup-with-invite is tracked in #145. Until then, show
  // a friendly "ask your admin" page.
  if (hydrated && setup?.signup_mode === 'invite_only') {
    return (
      <AuthShell>
        <h1 className="text-xl font-semibold text-gray-900 flex items-center gap-2">
          <Lock size={18} className="text-gray-500" />
          Signups are invite-only
        </h1>
        <p className="mt-2 text-sm text-gray-600">
          This instance only lets admins add new users. Ask your admin to send you an invite, then
          sign in with the credentials they share.
        </p>
        <div className="mt-6">
          <Link
            to="/signin"
            className="inline-flex items-center justify-center w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            Back to sign in
          </Link>
        </div>
      </AuthShell>
    )
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    clearError()
    setSubmitting(true)
    try {
      await signup(email, password)
    } catch {
      // error surfaces via store.
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthShell>
      <h1 className="text-xl font-semibold text-gray-900">Create your account</h1>
      <p className="mt-1 text-sm text-gray-500">
        Each user gets their own private book. Households are joined by invite later.
      </p>

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
          disabled={submitting || !email || !password}
          className="w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
        >
          {submitting ? 'Creating account…' : 'Sign up'}
        </button>
      </form>

      <div className="mt-4 text-center text-xs text-gray-500">
        Already have an account?{' '}
        <Link to="/signin" className="text-indigo-600 hover:text-indigo-700">Sign in</Link>
      </div>
    </AuthShell>
  )
}
