# ADR 0008: Household Aggregation Layer

## Status
Accepted

## Context
Four future household surfaces (Dashboard, Shared Budgets, Shared Goals, AI Advisor) all read across multiple members' financial data. If each surface reads the database directly, the privacy contract — "no PII, no raw transactions, no cross-member chat leakage" — has to be re-enforced four times and verified four times. We need a single point of enforcement.

## Decision

**One aggregator service** owns every household-scoped read: `internal/service/household/aggregator.go`.

```
type Aggregator struct {
    txRepo       repository.TransactionRepository
    acctRepo     repository.AccountRepository
    shareRepo    repository.AccountShareRepository
    memberRepo   repository.HouseholdMemberRepository
    sbRepo       repository.SharedBudgetRepository
    sgRepo       repository.SharedGoalRepository
    aiRepo       repository.AIThreadRepository
    now          func() time.Time
}
// NO pii_repo. Enforced at compile time, asserted in tests.
```

**One entry point per surface:**
- `Dashboard(ctx, householdID, period) → HouseholdDashboard`
- `BudgetPace(ctx, householdID, period) → []BudgetPaceItem`
- `GoalProgress(ctx, householdID) → []GoalProgressItem`
- `AIContext(ctx, householdID, requesterUserID) → HouseholdAIContext`

**Privacy contract** (verified by integration tests on every PR that touches the aggregator):

1. **No PII reaches the aggregator's output.** `pii_repo` is not in the dependency graph. A test asserts the import is absent from `internal/service/household/`.
2. **No raw transactions leave the aggregator.** Return types contain only aggregated values (`decimal.Decimal` sums, counts, percentages). A reflection-based test walks return-type fields and fails if any `model.Transaction` (or slice of) appears.
3. **`private` accounts are excluded** from every aggregate — net worth, category breakdown, cashflow, anywhere.
4. **`balance_only` accounts** contribute to balance/net-worth aggregates and member-contribution counts, but **not** to category breakdowns or transaction-derived metrics. Only `balance_and_txns` accounts feed category-level aggregations.
5. **In-grace members are excluded from live aggregates.** "Live" means current-period sums and pace calculations. Their historical contributions remain in long-window history (12-month net-worth lines, prior-period goal totals).
6. **AI context for member A never includes member B's non-shared threads.** `AIContext` reads `ai_threads WHERE household_id = ? AND shared_with_household = true` plus the requester's own threads — never another member's private threads. A test asserts this with seeded fixtures.

**The 4 household surfaces are the aggregator's only consumers.** Handler-layer code for `/h/dashboard`, `/h/budgets`, `/h/goals`, `/h/ai` does not query repositories directly.

## Rationale

- **Centralized enforcement scales.** Privacy invariants live in one file, one test suite, one set of types. Each new surface inherits the guarantees instead of re-deriving them.
- **Architectural enforcement over discipline.** Same posture as ADR-0003 (`pii_repo`) — make leaks impossible, not just discouraged. If a future contributor adds `pii_repo` to the aggregator's constructor, the package won't compile (because we don't import it) and the test asserts it.
- **Lazy lifecycle filtering belongs here.** ADR-0007 says the aggregator is the source of truth for "who counts right now." Putting that filter inside the aggregator means no surface can accidentally include in-grace members by writing its own query.
- **Reflection-based "no raw txns" test.** The cheapest, most reliable way to assert a structural property over time as new fields are added to return types.

## Consequences

- The aggregator becomes the single most security-sensitive package in the codebase. Every PR that touches `internal/service/household/` must keep the privacy test suite green.
- New household surfaces (post-M2.5) call the aggregator; they do not get repository handles.
- `shared_budgets` and `shared_goals` need read-only repos available to the aggregator from day one, even though their CRUD handlers ship later.
- AI thread sharing (`ai_threads.shared_with_household`) becomes part of the structural foundation, even though the AI surface itself ships in M7. The aggregator's `AIContext` needs a contract to test against.
- Reflection-based field check is one of the few places where compile-time guarantees aren't enough; the test must run on every CI build.
