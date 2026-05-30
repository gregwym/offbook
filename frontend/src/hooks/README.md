# Hooks

## `useScopedX()` convention

Personal and household scopes share the same pages under the v6 IA
(`docs/designs/App Hierarchy v6.html`): a surface like Budgets renders one
component at both `/budgets` and `/h/budgets`, and the **active scope swaps the
data source, not the page**. The `useScopedX()` hook is where that branch lives.

A scoped hook:

1. Reads `scopeStore` for `active` + `householdId` (and `hydrated`).
2. Fans out to the **personal** per-user endpoints when scope is personal, or
   the **household** endpoints when scope is household. Household reads go
   through the aggregator (`/h/...`) and the shared-CRUD APIs — a scoped hook
   never reads household repositories directly, preserving the cross-user-read
   rule in `.claude/rules/frontend.md` / ADR-0008.
3. Normalizes both responses to a single row shape so the page never branches
   on scope for rendering. Genuinely scope-specific fields (e.g. a personal
   goal's linked account, a household budget's role gating) are exposed as
   nullable/optional fields or flags, and the page renders them conditionally.
4. Re-fetches when the scope switches — `active`/`householdId` are in the
   effect dependency chain (via the `reload` callback).
5. Returns bound mutation handlers (`create`/`update`/`remove`/…) that already
   know the scope, plus:
   - `canMutate` — always `true` in personal scope; in household scope it
     reflects the member's role (owner/contributor can mutate; view_only
     cannot). The backend re-checks; this only hides controls that would 403.
   - `householdMissing` — `true` when in household scope with no household, so
     the page can show the "No household yet" empty state.

Examples: `useScopedBudgets.ts`, `useScopedGoals.ts`, `useScopedInsights.ts`.

Pages built on these hooks (`BudgetsPage`, `SavingsGoalsPage`, `InsightsPage`)
stay pure presentation and carry no `Household*Page.tsx` duplicate.
