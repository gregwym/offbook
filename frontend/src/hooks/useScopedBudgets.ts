// useScopedBudgets — single data hook backing the scope-agnostic BudgetsPage.
//
// The v6 IA (App Hierarchy v6) collapses the personal + household budget
// surfaces onto one component: the page is pure presentation, and this hook
// owns the scope branch. Personal scope reads the per-user budget endpoints;
// household scope reads the `shared_budgets` CRUD + the aggregator's budget
// pace. Both are normalized to a single `ScopedBudgetRow` shape so the page
// never branches on scope for rendering — only for the few genuinely
// divergent affordances (role-gated mutation, the household "paused" badge).
//
// Convention (see hooks/README.md): a `useScopedX()` hook reads `scopeStore`
// for `active`/`householdId`, fans out to the right endpoint, and re-fetches
// when the scope switches (scope is in the effect dep array). Cross-user reads
// stay inside the household aggregator — this hook never reads household repos
// directly; it only calls the existing `/h/...` aggregator + shared-CRUD APIs.
import { useCallback, useEffect, useState } from 'react'
import {
  createBudget,
  deleteBudget,
  getBudgetSpend,
  listBudgets,
  updateBudget,
} from '../api/budgets'
import {
  createSharedBudget,
  deleteSharedBudget,
  listSharedBudgets,
  updateSharedBudget,
} from '../api/households'
import { getBudgetPace } from '../api/householdAggregator'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'
import type { BudgetPeriod, BudgetSpend } from '../types/budget'
import { SCOPE_HOUSEHOLD } from '../types/scope'

// ScopedBudgetRow normalizes personal `Budget` + `BudgetSpend` and household
// `SharedBudget` + `BudgetPaceItem` into one shape. Spend fields are nullable:
// household pace lacks `remaining`/period bounds, and inactive personal
// budgets carry no spend.
export type ScopedBudgetRow = {
  id: number
  category_id: number
  period: BudgetPeriod
  amount: string
  rollover: boolean
  is_active: boolean
  spent: string | null
  remaining: string | null
  pct: number | null
  period_start: string | null
  period_end: string | null
}

// ScopedBudgetInput is the single create/update payload the page produces.
// The hook translates it to the personal or shared API shape.
export type ScopedBudgetInput = {
  category_id: number
  period: BudgetPeriod
  amount: string
  rollover: boolean
  is_active: boolean
}

export type UseScopedBudgets = {
  scope: 'personal' | 'household'
  rows: ScopedBudgetRow[]
  loading: boolean
  error: string | null
  canMutate: boolean
  householdMissing: boolean
  reload: () => Promise<void>
  create: (input: ScopedBudgetInput) => Promise<void>
  update: (id: number, input: ScopedBudgetInput) => Promise<void>
  remove: (id: number) => Promise<void>
  clearError: () => void
}

export function useScopedBudgets(): UseScopedBudgets {
  const { active, hydrated, householdId } = useScopeStore()
  const { detail, load: loadDetail } = useHouseholdStore()
  const isHousehold = active === SCOPE_HOUSEHOLD

  const [rows, setRows] = useState<ScopedBudgetRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // fetchRows is the pure loader (no setState) so both the effect and the
  // imperative reload share one data path.
  const fetchRows = useCallback(async (): Promise<ScopedBudgetRow[]> => {
    if (isHousehold) {
      if (householdId == null) return []
      return loadHousehold(householdId)
    }
    return loadPersonal()
  }, [isHousehold, householdId])

  // Household role drives `canMutate`. Load the household detail when we enter
  // household scope so the page can hide controls that would 403 server-side.
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
    async (input: ScopedBudgetInput) => {
      if (isHousehold) {
        if (householdId == null) return
        await createSharedBudget(householdId, input)
      } else {
        await createBudget(input)
      }
      await reload()
    },
    [isHousehold, householdId, reload],
  )

  const update = useCallback(
    async (id: number, input: ScopedBudgetInput) => {
      if (isHousehold) {
        if (householdId == null) return
        await updateSharedBudget(householdId, id, input)
      } else {
        await updateBudget(id, input)
      }
      await reload()
    },
    [isHousehold, householdId, reload],
  )

  const remove = useCallback(
    async (id: number) => {
      if (isHousehold) {
        if (householdId == null) return
        await deleteSharedBudget(householdId, id)
      } else {
        await deleteBudget(id)
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
    clearError: () => setError(null),
  }
}

async function loadPersonal(): Promise<ScopedBudgetRow[]> {
  const budgets = await listBudgets()
  // Spend is only meaningful for active budgets; fan out and drop row-level
  // failures so one bad spend fetch doesn't blank the page.
  const active = budgets.filter((b) => b.is_active)
  const results = await Promise.allSettled(active.map((b) => getBudgetSpend(b.id)))
  const spendById = new Map<number, BudgetSpend>()
  active.forEach((b, i) => {
    const r = results[i]
    if (r.status === 'fulfilled') spendById.set(b.id, r.value)
  })
  return budgets.map((b) => {
    const s = spendById.get(b.id)
    return {
      id: b.id,
      category_id: b.category_id,
      period: b.period,
      amount: b.amount,
      rollover: b.rollover,
      is_active: b.is_active,
      spent: s?.spent ?? null,
      remaining: s?.remaining ?? null,
      pct: s ? s.pct : null,
      period_start: s?.period_start ?? null,
      period_end: s?.period_end ?? null,
    }
  })
}

async function loadHousehold(householdId: number): Promise<ScopedBudgetRow[]> {
  const [budgets, pace] = await Promise.all([
    listSharedBudgets(householdId),
    getBudgetPace('current_month'),
  ])
  const paceById = new Map(pace.map((p) => [p.budget_id, p]))
  return budgets.map((b) => {
    const p = paceById.get(b.id)
    return {
      id: b.id,
      category_id: b.category_id,
      period: b.period,
      amount: b.amount,
      rollover: b.rollover,
      is_active: b.is_active,
      spent: p?.spent ?? null,
      remaining: null,
      pct: p ? Number.parseFloat(p.pace) || 0 : null,
      period_start: null,
      period_end: null,
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
