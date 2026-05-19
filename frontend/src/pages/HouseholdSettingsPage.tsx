import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Building2, DoorOpen, UserCog } from 'lucide-react'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'

export function HouseholdSettingsPage() {
  const { householdId } = useScopeStore()
  const hydrateScope = useScopeStore((s) => s.hydrate)
  const { detail, loading, error, load, updateHousehold, leave, clearError } =
    useHouseholdStore()
  const navigate = useNavigate()
  // Form fields default to "use the persisted detail" via null. As soon as
  // the user types, the override is non-null and we render that. Save
  // resets to null. Avoids a setState-in-effect lint violation while
  // keeping the form in sync with whichever household is loaded.
  const [nameEdit, setNameEdit] = useState<string | null>(null)
  const [graceEdit, setGraceEdit] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [leaving, setLeaving] = useState(false)

  useEffect(() => {
    if (householdId != null) void load(householdId)
  }, [householdId, load])

  const name = nameEdit ?? detail?.household.name ?? ''
  const grace = graceEdit ?? (detail ? String(detail.household.grace_period_days) : '30')

  if (householdId == null) {
    return (
      <div className="rounded-lg border border-gray-200 bg-white p-8 text-center">
        <Building2 size={28} className="mx-auto text-gray-300 mb-2" />
        <h1 className="text-base font-medium text-gray-900">No household yet</h1>
        <p className="text-sm text-gray-500 mt-1">
          Use the scope switcher in the sidebar to create or join a household.
        </p>
      </div>
    )
  }

  if (loading && !detail) {
    return <div className="text-center text-sm text-gray-400 py-6">Loading…</div>
  }

  if (!detail) {
    return <div className="text-sm text-red-600">{error || 'Household not found.'}</div>
  }

  const isOwner = detail.role === 'owner'
  const dirty =
    detail.household.name !== name.trim() ||
    String(detail.household.grace_period_days) !== grace.trim()

  const save = async () => {
    if (!householdId) return
    setSaving(true)
    try {
      const graceNum = Number.parseInt(grace, 10)
      await updateHousehold(householdId, {
        name: name.trim() !== detail.household.name ? name.trim() : undefined,
        grace_period_days: Number.isFinite(graceNum) ? graceNum : undefined,
      })
      // Reset edits so the next render shows the freshly-loaded values.
      setNameEdit(null)
      setGraceEdit(null)
    } finally {
      setSaving(false)
    }
  }

  const handleLeave = async () => {
    if (!householdId) return
    if (
      !window.confirm(
        `Leave "${detail.household.name}"? Your shared aggregates pause now. ` +
          `You can rejoin within ${detail.household.grace_period_days} days via a fresh invite and ` +
          `your prior shares auto-resume.`,
      )
    ) {
      return
    }
    setLeaving(true)
    try {
      await leave(householdId)
      // After leaving, scope must reset to personal — re-hydrate the
      // scope store so the sidebar swaps + the page redirects.
      await hydrateScope()
      navigate('/dashboard', { replace: true })
    } finally {
      setLeaving(false)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-gray-900">Household settings</h1>
        <p className="mt-1 text-sm text-gray-500">
          {detail.household.name} · you are <strong>{detail.role.replace('_', '-')}</strong>
        </p>
      </div>

      {error && (
        <div className="flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <section className="rounded-lg border border-gray-200 bg-white">
        <header className="flex items-center gap-2 border-b border-gray-200 px-5 py-3">
          <Building2 size={16} className="text-gray-500" />
          <h2 className="text-base font-medium text-gray-900">Profile</h2>
        </header>
        <div className="px-5 py-4 space-y-4">
          <label className="block text-sm">
            <div className="font-medium text-gray-700 mb-1">Name</div>
            <input
              type="text"
              disabled={!isOwner}
              value={name}
              onChange={(e) => setNameEdit(e.target.value)}
              className="w-full max-w-md rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none disabled:bg-gray-50 disabled:text-gray-500"
            />
          </label>
          <label className="block text-sm">
            <div className="font-medium text-gray-700 mb-1">Grace period (days)</div>
            <input
              type="number"
              min={0}
              max={365}
              disabled={!isOwner}
              value={grace}
              onChange={(e) => setGraceEdit(e.target.value)}
              className="w-32 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none disabled:bg-gray-50 disabled:text-gray-500"
            />
            <div className="mt-1 text-xs text-gray-500">
              How long a member's shares are preserved after they leave, so they can rejoin without
              losing history.
            </div>
          </label>
          {isOwner && (
            <div className="flex justify-end">
              <button
                type="button"
                disabled={!dirty || saving}
                onClick={() => void save()}
                className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
              >
                {saving ? 'Saving…' : 'Save changes'}
              </button>
            </div>
          )}
        </div>
      </section>

      {isOwner && (
        <section className="rounded-lg border border-gray-200 bg-white">
          <header className="flex items-center gap-2 border-b border-gray-200 px-5 py-3">
            <UserCog size={16} className="text-gray-500" />
            <h2 className="text-base font-medium text-gray-900">Ownership</h2>
          </header>
          <div className="px-5 py-4">
            <button
              type="button"
              disabled
              title="Backend endpoint pending — tracked in #152"
              className="rounded-md border border-gray-300 bg-gray-50 px-3 py-1.5 text-sm font-medium text-gray-400 cursor-not-allowed"
            >
              Transfer ownership…
            </button>
            <p className="mt-1 text-xs text-gray-500">
              Coming in #152. Until then, the sole owner cannot leave (the `LAST_OWNER` guard returns
              a conflict — dissolve the household instead).
            </p>
          </div>
        </section>
      )}

      <section className="rounded-lg border border-red-200 bg-white">
        <header className="flex items-center gap-2 border-b border-red-200 px-5 py-3">
          <DoorOpen size={16} className="text-red-600" />
          <h2 className="text-base font-medium text-red-700">Leave household</h2>
        </header>
        <div className="px-5 py-4">
          <p className="text-sm text-gray-600">
            Your shared aggregates pause immediately. Historical data stays in the household for{' '}
            {detail.household.grace_period_days} days — you can rejoin with a fresh invite to
            auto-resume your shares.
          </p>
          <button
            type="button"
            onClick={() => void handleLeave()}
            disabled={leaving}
            className="mt-3 inline-flex items-center gap-2 rounded-md border border-red-300 bg-white px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 disabled:opacity-50"
          >
            <DoorOpen size={14} />
            {leaving ? 'Leaving…' : 'Leave household'}
          </button>
        </div>
      </section>
    </div>
  )
}
