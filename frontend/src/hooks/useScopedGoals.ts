// useScopedGoals — single data hook backing the scope-agnostic
// SavingsGoalsPage. Same convention as useScopedBudgets (see hooks/README.md):
// the hook owns the scope branch, the page stays pure.
//
// Personal scope reads the per-user savings-goal endpoints (which already
// return server-computed `progress_pct`/`remaining` and an optional linked
// account). Household scope reads `shared_goals` CRUD + the aggregator's
// goal-progress endpoint. Linked accounts and `remaining` are personal-only
// concepts — they normalize to `null` in household scope and the page hides
// the corresponding affordances. Cross-user reads stay inside the aggregator;
// this hook only calls the existing `/h/...` APIs.
import { useCallback, useEffect, useState } from 'react'
import {
  contributeToGoal,
  createGoal,
  deleteGoal,
  listGoals,
  updateGoal,
} from '../api/savingsGoals'
import {
  contributeToSharedGoal,
  createSharedGoal,
  deleteSharedGoal,
  listSharedGoals,
  updateSharedGoal,
} from '../api/households'
import { getGoalProgress } from '../api/householdAggregator'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'
import type {
  CreateSharedGoalInput,
  UpdateSharedGoalInput,
} from '../types/household'
import type { UpdateGoalInput } from '../types/savingsGoal'
import { SCOPE_HOUSEHOLD } from '../types/scope'

// ScopedGoalRow normalizes personal `SavingsGoal` and household `SharedGoal`
// (+ `GoalProgressItem`). `remaining` and `account_id` are personal-only.
export type ScopedGoalRow = {
  id: number
  name: string
  target_amount: string
  current_amount: string
  target_date: string | null
  progress_pct: number // 0..1
  remaining: string | null
  account_id: number | null
}

// ScopedGoalInput is the single create/update payload the page produces. The
// hook translates it (incl. clear-flags for edits) to the personal or shared
// API shape.
export type ScopedGoalInput = {
  name: string
  target_amount: string
  target_date: string | null
  account_id: number | null
}

export type UseScopedGoals = {
  scope: 'personal' | 'household'
  rows: ScopedGoalRow[]
  loading: boolean
  error: string | null
  canMutate: boolean
  householdMissing: boolean
  reload: () => Promise<void>
  create: (input: ScopedGoalInput) => Promise<void>
  update: (id: number, input: ScopedGoalInput) => Promise<void>
  remove: (id: number) => Promise<void>
  contribute: (id: number, amount: string) => Promise<void>
  clearError: () => void
}

export function useScopedGoals(): UseScopedGoals {
  const { active, hydrated, householdId } = useScopeStore()
  const { detail, load: loadDetail } = useHouseholdStore()
  const isHousehold = active === SCOPE_HOUSEHOLD

  const [rows, setRows] = useState<ScopedGoalRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // fetchRows is the pure loader (no setState) so both the effect and the
  // imperative reload share one data path.
  const fetchRows = useCallback(async (): Promise<ScopedGoalRow[]> => {
    if (isHousehold) {
      if (householdId == null) return []
      return loadHousehold(householdId)
    }
    return loadPersonal()
  }, [isHousehold, householdId])

  useEffect(() => {
    if (!hydrated) return
    if (isHousehold && householdId != null) void loadDetail(householdId)
  }, [hydrated, isHousehold, householdId, loadDetail])

  // Initial + scope-switch load. State is only set after the await so we don't
  // trip react-hooks/set-state-in-effect; the imperative reload below handles
  // post-mutation refreshes.
  useEffect(() => {
    if (!hydrated) return
    let cancelled = false
    void (async () => {
      try {
        const next = await fetchRows()
        if (!cancelled) {
          setRows(next)
          setError(null)
        }
      } catch (e) {
        if (!cancelled) setError(errMsg(e))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [hydrated, fetchRows])

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setRows(await fetchRows())
      setError(null)
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [fetchRows])

  const create = useCallback(
    async (input: ScopedGoalInput) => {
      if (isHousehold) {
        if (householdId == null) return
        const payload: CreateSharedGoalInput = {
          name: input.name,
          target_amount: input.target_amount,
        }
        if (input.target_date) payload.target_date = input.target_date
        await createSharedGoal(householdId, payload)
      } else {
        await createGoal({
          name: input.name,
          target_amount: input.target_amount,
          target_date: input.target_date,
          account_id: input.account_id,
        })
      }
      await reload()
    },
    [isHousehold, householdId, reload],
  )

  const update = useCallback(
    async (id: number, input: ScopedGoalInput) => {
      if (isHousehold) {
        if (householdId == null) return
        const patch: UpdateSharedGoalInput = {
          name: input.name,
          target_amount: input.target_amount,
        }
        if (input.target_date) patch.target_date = input.target_date
        else patch.clear_target_date = true
        await updateSharedGoal(householdId, id, patch)
      } else {
        const patch: UpdateGoalInput = {
          name: input.name,
          target_amount: input.target_amount,
        }
        if (input.target_date) patch.target_date = input.target_date
        else patch.clear_target_date = true
        if (input.account_id != null) patch.account_id = input.account_id
        else patch.clear_account_id = true
        await updateGoal(id, patch)
      }
      await reload()
    },
    [isHousehold, householdId, reload],
  )

  const remove = useCallback(
    async (id: number) => {
      if (isHousehold) {
        if (householdId == null) return
        await deleteSharedGoal(householdId, id)
      } else {
        await deleteGoal(id)
      }
      await reload()
    },
    [isHousehold, householdId, reload],
  )

  const contribute = useCallback(
    async (id: number, amount: string) => {
      if (isHousehold) {
        if (householdId == null) return
        await contributeToSharedGoal(householdId, id, amount)
      } else {
        await contributeToGoal(id, { amount })
      }
      await reload()
    },
    [isHousehold, householdId, reload],
  )

  const householdMissing = isHousehold && householdId == null
  const canMutate = isHousehold
    ? detail?.role === 'owner' || detail?.role === 'contributor'
    : true

  return {
    scope: isHousehold ? 'household' : 'personal',
    rows,
    loading,
    error,
    canMutate,
    householdMissing,
    reload,
    create,
    update,
    remove,
    contribute,
    clearError: () => setError(null),
  }
}

async function loadPersonal(): Promise<ScopedGoalRow[]> {
  const goals = await listGoals()
  return goals.map((g) => ({
    id: g.id,
    name: g.name,
    target_amount: g.target_amount,
    current_amount: g.current_amount,
    target_date: g.target_date ?? null,
    progress_pct: g.progress_pct,
    remaining: g.remaining,
    account_id: g.account_id ?? null,
  }))
}

async function loadHousehold(householdId: number): Promise<ScopedGoalRow[]> {
  const [goals, progress] = await Promise.all([
    listSharedGoals(householdId),
    getGoalProgress(),
  ])
  const progressById = new Map(progress.map((p) => [p.goal_id, p]))
  return goals.map((g) => {
    const p = progressById.get(g.id)
    return {
      id: g.id,
      name: g.name,
      target_amount: g.target_amount,
      current_amount: g.current_amount,
      target_date: g.target_date ?? null,
      progress_pct: p ? Number.parseFloat(p.progress) || 0 : 0,
      remaining: null,
      account_id: null,
    }
  })
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
