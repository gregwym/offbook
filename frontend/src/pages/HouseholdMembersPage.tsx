import { useEffect, useState } from 'react'
import { Check, Copy, Mail, Users } from 'lucide-react'
import { useAuthStore } from '../store/authStore'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'
import type { HouseholdRole } from '../types/household'

export function HouseholdMembersPage() {
  const { householdId } = useScopeStore()
  const meID = useAuthStore((s) => s.user?.id ?? null)
  const { detail, loading, error, load, lastInvite, invite, clearInvite, clearError } =
    useHouseholdStore()
  const [inviting, setInviting] = useState(false)

  useEffect(() => {
    if (householdId != null) void load(householdId)
  }, [householdId, load])

  if (householdId == null) {
    return (
      <Section>
        <div className="text-center py-8">
          <Users size={28} className="mx-auto text-gray-300 mb-2" />
          <h1 className="text-base font-medium text-gray-900">No household yet</h1>
          <p className="text-sm text-gray-500 mt-1">
            Use the scope switcher in the sidebar to create or join a household.
          </p>
        </div>
      </Section>
    )
  }

  if (loading && !detail) {
    return <Section><div className="text-center text-sm text-gray-400 py-6">Loading…</div></Section>
  }

  if (!detail) {
    return (
      <Section>
        <div className="text-sm text-red-600 px-5 py-3">{error || 'Household not found.'}</div>
      </Section>
    )
  }

  const isOwner = detail.role === 'owner'

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">{detail.household.name}</h1>
          <p className="mt-1 text-sm text-gray-500">
            {detail.members.length} member{detail.members.length === 1 ? '' : 's'} · grace period{' '}
            {detail.household.grace_period_days} days
          </p>
        </div>
        {isOwner && (
          <button
            type="button"
            onClick={() => setInviting(true)}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Mail size={16} /> Invite member
          </button>
        )}
      </div>

      {error && (
        <div className="flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <Section>
        <div className="divide-y divide-gray-100">
          {detail.members.map((m) => (
            <div key={m.id} className="flex items-center justify-between px-5 py-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-gray-900 truncate">User #{m.user_id}</span>
                  {m.user_id === meID && (
                    <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-700">you</span>
                  )}
                </div>
                <div className="mt-0.5 text-xs text-gray-500">
                  Joined {new Date(m.joined_at).toLocaleDateString()}
                </div>
              </div>
              <RoleChip role={m.role} />
            </div>
          ))}
        </div>
      </Section>

      <p className="text-xs text-gray-500">
        Owner-side member moderation (change role, remove, in-grace listing) is tracked in #147
        and lands when the backend endpoints exist.
      </p>

      {inviting && (
        <InviteModal
          onClose={() => {
            setInviting(false)
            clearInvite()
          }}
          onMint={async (role) => {
            await invite(householdId, role)
          }}
          lastInvite={lastInvite}
        />
      )}
    </div>
  )
}

function Section({ children }: { children: React.ReactNode }) {
  return <section className="rounded-lg border border-gray-200 bg-white">{children}</section>
}

function RoleChip({ role }: { role: HouseholdRole }) {
  const styles: Record<HouseholdRole, string> = {
    owner: 'bg-indigo-100 text-indigo-800',
    contributor: 'bg-emerald-100 text-emerald-800',
    view_only: 'bg-gray-100 text-gray-700',
  }
  const labels: Record<HouseholdRole, string> = {
    owner: 'Owner',
    contributor: 'Contributor',
    view_only: 'View-only',
  }
  return (
    <span className={['rounded-full px-2.5 py-1 text-xs font-medium', styles[role]].join(' ')}>
      {labels[role]}
    </span>
  )
}

function InviteModal({
  onClose,
  onMint,
  lastInvite,
}: {
  onClose: () => void
  onMint: (role: HouseholdRole) => Promise<void>
  lastInvite: ReturnType<typeof useHouseholdStore.getState>['lastInvite']
}) {
  const [role, setRole] = useState<HouseholdRole>('contributor')
  const [submitting, setSubmitting] = useState(false)
  const [copied, setCopied] = useState(false)

  const mint = async () => {
    setSubmitting(true)
    try {
      await onMint(role)
    } finally {
      setSubmitting(false)
    }
  }

  const copyToken = async () => {
    if (!lastInvite?.token) return
    try {
      await navigator.clipboard.writeText(lastInvite.token)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard API can fail in non-secure contexts — operator can still
      // select + copy manually.
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
        <div className="border-b border-gray-200 px-5 py-3">
          <h3 className="text-base font-medium text-gray-900">Invite a member</h3>
        </div>
        <div className="px-5 py-4 space-y-4">
          {!lastInvite && (
            <>
              <label className="block text-sm">
                <div className="font-medium text-gray-700 mb-1">Role</div>
                <select
                  value={role}
                  onChange={(e) => setRole(e.target.value as HouseholdRole)}
                  className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
                >
                  <option value="contributor">Contributor (can add shared budgets/goals)</option>
                  <option value="view_only">View-only (read-only access)</option>
                </select>
              </label>
              <p className="text-xs text-gray-500">
                Mints a one-time token. Share it with the invitee — they sign in and accept the
                invite to join.
              </p>
            </>
          )}

          {lastInvite && (
            <div className="space-y-2">
              <div className="text-sm font-medium text-gray-800">Invite created!</div>
              <p className="text-xs text-gray-500">
                Share this token with the invitee. They sign in and accept it (the household-join
                modal from #144 will handle this in-app).
              </p>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded bg-gray-50 px-2 py-1.5 font-mono text-xs text-gray-800 break-all">
                  {lastInvite.token}
                </code>
                <button
                  type="button"
                  onClick={() => void copyToken()}
                  className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50"
                >
                  {copied ? <Check size={14} /> : <Copy size={14} />}
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
              <div className="text-[11px] text-gray-400">
                Expires {new Date(lastInvite.invite.expires_at).toLocaleString()}.
              </div>
            </div>
          )}
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3">
          {!lastInvite && (
            <button
              type="button"
              onClick={() => void mint()}
              disabled={submitting}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              {submitting ? 'Minting…' : 'Mint invite'}
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            {lastInvite ? 'Done' : 'Cancel'}
          </button>
        </div>
      </div>
    </div>
  )
}
