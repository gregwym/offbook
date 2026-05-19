import { useCallback, useEffect, useState } from 'react'
import { Check, Copy, Mail, Trash2, Users } from 'lucide-react'
import {
  createInvite,
  listMembers,
  removeMember,
  updateMemberRole,
} from '../api/households'
import { useAuthStore } from '../store/authStore'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'
import type {
  CreateInviteResult,
  HouseholdMember,
  HouseholdRole,
  MembersListing,
} from '../types/household'

const ROLE_LABELS: Record<HouseholdRole, string> = {
  owner: 'Owner',
  contributor: 'Contributor',
  view_only: 'View-only',
}

const ROLE_STYLES: Record<HouseholdRole, string> = {
  owner: 'bg-indigo-100 text-indigo-800',
  contributor: 'bg-emerald-100 text-emerald-800',
  view_only: 'bg-gray-100 text-gray-700',
}

export function HouseholdMembersPage() {
  const { householdId } = useScopeStore()
  const meID = useAuthStore((s) => s.user?.id ?? null)
  const { detail, loading, error, load, clearError } = useHouseholdStore()
  const [listing, setListing] = useState<MembersListing | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [inviting, setInviting] = useState(false)
  const [lastInvite, setLastInvite] = useState<CreateInviteResult | null>(null)
  const [busyUserID, setBusyUserID] = useState<number | null>(null)

  const reloadListing = useCallback(() => {
    if (householdId == null) return Promise.resolve()
    return listMembers(householdId, true)
      .then((res) => {
        setListing(res)
        setListError(null)
      })
      .catch((e: unknown) => setListError(errMsg(e)))
  }, [householdId])

  useEffect(() => {
    if (householdId != null) {
      void load(householdId)
      void reloadListing()
    }
  }, [householdId, load, reloadListing])

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
  const active = listing?.active ?? []
  const inGrace = listing?.in_grace ?? []

  const handleMintInvite = async (role: HouseholdRole) => {
    const res = await createInvite(householdId, role)
    setLastInvite(res)
    // Also surfaces in /h/dashboard via reload.
    return res
  }

  const handleRoleChange = async (member: HouseholdMember, role: HouseholdRole) => {
    if (role === member.role) return
    setBusyUserID(member.user_id)
    setListError(null)
    try {
      await updateMemberRole(householdId, member.user_id, role)
      await reloadListing()
    } catch (e) {
      setListError(errMsg(e))
    } finally {
      setBusyUserID(null)
    }
  }

  const handleRemove = async (member: HouseholdMember) => {
    if (!window.confirm(`Remove member #${member.user_id}? They enter the grace window and can rejoin via a fresh invite.`)) return
    setBusyUserID(member.user_id)
    setListError(null)
    try {
      await removeMember(householdId, member.user_id)
      await reloadListing()
    } catch (e) {
      setListError(errMsg(e))
    } finally {
      setBusyUserID(null)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">{detail.household.name}</h1>
          <p className="mt-1 text-sm text-gray-500">
            {active.length} active member{active.length === 1 ? '' : 's'}
            {inGrace.length > 0 ? ` · ${inGrace.length} in grace` : ''} · grace period{' '}
            {detail.household.grace_period_days} days
          </p>
        </div>
        {isOwner && (
          <button
            type="button"
            onClick={() => {
              setInviting(true)
              setLastInvite(null)
            }}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Mail size={16} /> Invite member
          </button>
        )}
      </div>

      {(error || listError) && (
        <div className="flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{listError || error}</span>
          <button
            type="button"
            onClick={() => {
              setListError(null)
              clearError()
            }}
            className="ml-3 text-red-600 hover:text-red-800"
          >
            ×
          </button>
        </div>
      )}

      <Section title="Active members">
        <div className="divide-y divide-gray-100">
          {active.map((m) => (
            <MemberRow
              key={m.id}
              member={m}
              isMe={m.user_id === meID}
              isOwnerOfHousehold={isOwner}
              busy={busyUserID === m.user_id}
              onRoleChange={handleRoleChange}
              onRemove={handleRemove}
            />
          ))}
        </div>
      </Section>

      {inGrace.length > 0 && (
        <Section title={`In grace · ${inGrace.length}`}>
          <div className="divide-y divide-gray-100">
            {inGrace.map((m) => (
              <div key={m.id} className="px-5 py-3 flex items-center justify-between">
                <div className="min-w-0">
                  <div className="font-medium text-gray-900">User #{m.user_id}</div>
                  <div className="text-xs text-gray-500">
                    Left {m.left_at ? new Date(m.left_at).toLocaleDateString() : 'recently'} ·
                    can rejoin via a fresh invite within {detail.household.grace_period_days} days
                  </div>
                </div>
                <RoleChip role={m.role} />
              </div>
            ))}
          </div>
        </Section>
      )}

      {inviting && (
        <InviteModal
          onClose={() => {
            setInviting(false)
            setLastInvite(null)
          }}
          onMint={handleMintInvite}
          lastInvite={lastInvite}
        />
      )}
    </div>
  )
}

function MemberRow({
  member,
  isMe,
  isOwnerOfHousehold,
  busy,
  onRoleChange,
  onRemove,
}: {
  member: HouseholdMember
  isMe: boolean
  isOwnerOfHousehold: boolean
  busy: boolean
  onRoleChange: (m: HouseholdMember, role: HouseholdRole) => Promise<void>
  onRemove: (m: HouseholdMember) => Promise<void>
}) {
  // Owner controls only appear for non-self rows. The backend rejects
  // self-modification on these endpoints with `CANNOT_MODIFY_SELF`.
  const showControls = isOwnerOfHousehold && !isMe

  return (
    <div className="flex items-center justify-between px-5 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-medium text-gray-900 truncate">User #{member.user_id}</span>
          {isMe && (
            <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-700">you</span>
          )}
        </div>
        <div className="mt-0.5 text-xs text-gray-500">
          Joined {new Date(member.joined_at).toLocaleDateString()}
        </div>
      </div>
      {showControls ? (
        <div className="flex items-center gap-2">
          <select
            value={member.role}
            disabled={busy}
            onChange={(e) => void onRoleChange(member, e.target.value as HouseholdRole)}
            className="rounded-md border border-gray-300 px-2 py-1 text-xs focus:border-indigo-500 focus:outline-none disabled:opacity-50"
          >
            <option value="owner">Owner</option>
            <option value="contributor">Contributor</option>
            <option value="view_only">View-only</option>
          </select>
          <button
            type="button"
            onClick={() => void onRemove(member)}
            disabled={busy}
            aria-label="Remove member"
            className="rounded-md border border-gray-300 px-2 py-1 text-gray-500 hover:bg-red-50 hover:text-red-700 disabled:opacity-50"
          >
            <Trash2 size={14} />
          </button>
        </div>
      ) : (
        <RoleChip role={member.role} />
      )}
    </div>
  )
}

function Section({ title, children }: { title?: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      {title && (
        <header className="border-b border-gray-200 px-5 py-2 text-xs font-semibold uppercase tracking-wider text-gray-500">
          {title}
        </header>
      )}
      {children}
    </section>
  )
}

function RoleChip({ role }: { role: HouseholdRole }) {
  return (
    <span className={['rounded-full px-2.5 py-1 text-xs font-medium', ROLE_STYLES[role]].join(' ')}>
      {ROLE_LABELS[role]}
    </span>
  )
}

function InviteModal({
  onClose,
  onMint,
  lastInvite,
}: {
  onClose: () => void
  onMint: (role: HouseholdRole) => Promise<CreateInviteResult>
  lastInvite: CreateInviteResult | null
}) {
  const [role, setRole] = useState<HouseholdRole>('contributor')
  const [submitting, setSubmitting] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const mint = async () => {
    setSubmitting(true)
    setError(null)
    try {
      await onMint(role)
    } catch (e) {
      setError(errMsg(e))
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
      // Clipboard API unavailable in non-secure contexts — manual select still works.
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
                Mints a one-time token. Share it with the invitee — they accept on /signup (invite-only
                instances) or via the household-join modal in the scope picker.
              </p>
              {error && (
                <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{error}</div>
              )}
            </>
          )}

          {lastInvite && (
            <div className="space-y-2">
              <div className="text-sm font-medium text-gray-800">Invite created!</div>
              <p className="text-xs text-gray-500">
                Share this token. Brand-new users sign up at /signup with it; existing users can
                accept via the household-join modal in the scope picker.
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

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
