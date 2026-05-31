# ADR-0018: Unified Scoped Plans — One Budget / Goal Table Per Concept, Owner = User XOR Household

**Status:** Accepted

**Context date:** 2026-05-31

## Context

Budgets and savings goals each exist twice in the schema and the code:

| Concept | Personal | Household |
|---|---|---|
| Config table | `budgets (user_id, category_id, period, amount, rollover, is_active)` | `shared_budgets (household_id, …)` — identical columns |
| | `savings_goals (user_id, name, target_amount, current_amount, target_date, account_id)` | `shared_goals (household_id, …)` — **missing `account_id`** |
| CRUD/validation | `service/budget_service.go`, `service/savings_goal_service.go` | `service/household/shared_budgets.go`, `shared_goals.go` (~90% copies) |
| Evaluation | `BudgetService.Spend/Alerts`, goal progress | `Aggregator.BudgetPace/GoalProgress` |
| Repo | `budget_repo.go`, `savings_goal_repo.go` | `shared_budget_repo.go`, `shared_goal_repo.go` |
| Frontend | `useScopedBudgets`, `useScopedGoals` already render **one** component and swap the data source |

This is the M2.5 households foundation (ADR-0006) landing the household variants as parallel tables/services. It works, but it is **two layers of duplication stacked**, and only one layer is incidental:

1. **Configuration** (the columns: amount, category, period, name, target) — genuinely identical between scopes. This is pure copy, and it has already drifted: `shared_goals` silently lost `account_id`, and only the personal budget path surfaces a friendly `ErrDuplicateActiveBudget` (the shared path returns a raw FK/constraint error). Each new field or rule has to be written twice and the copies rot.
2. **Evaluation** (spend / pace / progress) — *looks* parallel but is fundamentally different. Personal evaluation reads the user's own transactions directly. Household evaluation runs in `service/household/aggregator.go` because it **must** honor per-account visibility (`balance_only` excluded from category aggregates), exclude in-grace members from live aggregates, and never return a raw transaction row or PII (ADR-0008).

A standing observation prompted this ADR: "can member and household share the same budget/goal tables? scope is defined by the associated accounts. alternatively, merge household with user?" The answer requires separating those two layers.

## Decision

**Unify the configuration; keep evaluation scope-aware. Do not merge `household` into `user` as a generic principal.**

### 1. One table per concept, owner = user XOR household

Collapse `shared_budgets` → `budgets` and `shared_goals` → `savings_goals`:

- Add `household_id BIGINT NULL REFERENCES households(id)`.
- Make `user_id BIGINT NULL` (was `NOT NULL`).
- `CHECK ((user_id IS NOT NULL) <> (household_id IS NOT NULL))` — **exactly one** owner is set. A row is either a personal plan or a household plan, never both, never neither.
- Per-owner partial unique indexes carry the existing semantics across both owners:
  - budgets: `UNIQUE (user_id, category_id, period) WHERE deleted_at IS NULL AND is_active AND user_id IS NOT NULL` and the matching `household_id` variant.
- `savings_goals.account_id` stays, and is **only valid when `user_id IS NOT NULL`** — a household goal has no single owning account (it spans members' shared accounts). Enforced by CHECK: `account_id IS NULL OR user_id IS NOT NULL`. This *restores* the `account_id` the household copy had dropped, instead of perpetuating the drift.

`shared_budgets`, `shared_goals` and their repos are deleted.

**Scope is the account set, supplied at evaluation time — not stored on the plan.** A budget is category-scoped and account-agnostic in its config; the *scope* decides which transactions feed the spend calc: personal scope = the owner's accounts, household scope = accounts shared into the household (honoring visibility). The plan row never needs to enumerate accounts. This is exactly the "scope is defined by the associated accounts" mental model — it is already how the system computes, and unifying the table makes the config match the computation.

### 2. One config-CRUD path; authz is a thin scope pre-check

Validation, period rules, amount checks, sparse-patch update, soft-delete, and the duplicate-active friendly error live **once** per concept. The only scope-dependent step is authorization, applied as a guard before the shared logic:

- **Personal:** the owner is the session user (`user_id` derived from session, never the body). Read/write your own row.
- **Household:** `household_id` from the path; role check — owner/contributor may write, `view_only` is read-only, non-members rejected (`requireContributor`, as today).

### 3. Evaluation stays polymorphic — the line we do not erase

Personal spend/progress is computed directly from the owner's transactions. Household spend/progress continues to run through `service/household/aggregator.go`, preserving the ADR-0008 privacy guarantees structurally (the aggregator package cannot import `pii_repo` and returns no raw rows). Unifying *config* storage does not move the *evaluation* of household plans out of the aggregator.

### 4. Rejected: merge `household` into `user` (generic principal / `owner_id`)

The tempting next step — one `principals`/`owners` table, every domain row carries `owner_id`, a household is "just another owner" — is **rejected**. The asymmetry between personal and household is not incidental; it *is* the privacy invariant:

- **Personal = direct ownership.** `user_id NOT NULL` on every domain row, read your own data.
- **Household = derived, opt-in, aggregated-only.** Cross-user reads happen *only* through the aggregator, *only* within one household, *only* for accounts explicitly shared, *never* returning raw rows or PII (ADR-0006, ADR-0008).

Today that boundary is **structural**: `service/household/` is a separate package that physically cannot reach `pii_repo`, and "cross-user reads only via the aggregator" is enforceable at the package level. A generic `owner_id` collapses the two into one access path and replaces that compile-time/package-level guarantee with a runtime convention every query author must remember. For a privacy-first app that is a bad trade — we would be tidying the schema by demoting a safety property. The unification in §1 deliberately stops at the **config** row (which carries no transactions and no PII) and leaves the **read paths** asymmetric.

## Consequences

- One table, one repo, one validated CRUD path per concept. New fields/rules are written once; the personal/household copies can no longer drift.
- `savings_goals.account_id` is restored for household goals' personal counterpart and explicitly constrained, fixing the M2.5 drift.
- The `<>` (XOR) CHECK makes "a plan belongs to exactly one scope" a database invariant rather than an application assumption.
- The privacy boundary is unchanged: household plans are still evaluated only through the aggregator, and there is still no generic cross-user reader.
- Cost: a migration that rewrites two tables and a refactor that merges four services into two (plus their repos/handlers/tests). Pre-prod (dev DBs are wiped and rebuilt — see ROADMAP M10), so no data migration is required; `shared_*` rows are dropped with the tables.

## Rollout

Staged to keep PRs reviewable:

1. **This ADR** — its own PR, reviewed before code moves.
2. **Migration** — unify `budgets` + `savings_goals` (add `household_id`, nullable `user_id`, XOR + account CHECKs, per-owner partial unique indexes), drop `shared_budgets`/`shared_goals`; regenerate `docs/db/schema.md` (ADR / #304 tooling).
3. **Code unification** — one config-CRUD path per concept with scope-dependent authz; household evaluation stays in the aggregator; update models, repos, handlers, router, and the frontend `api`/hooks. `make verify` green, aggregator privacy tests green.

Tracked in [#306](https://github.com/gregwym/offbook/issues/306).
