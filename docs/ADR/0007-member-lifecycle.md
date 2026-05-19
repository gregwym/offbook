# ADR 0007: Member Lifecycle — Leave, Grace, Rejoin, Purge

## Status
Accepted

## Context
Household membership changes over time. People move out, take a break, or come back. The product spec (`App Hierarchy v4.html`, section 06) settled on a self-service-leave model with a grace period that lets rejoining "just work" without re-setup. This ADR ratifies the state machine, the data shape, and the read-side enforcement.

## Decision

**State machine** for a row in `household_members`:

| Phase | `left_at` | `purged_at` | In live aggregates? | In historical aggregates? |
|---|---|---|---|---|
| Active | NULL | NULL | Yes | Yes |
| In grace | NOT NULL, within grace window | NULL | **No** | Yes |
| Purged | NOT NULL | NOT NULL | No | **Yes** (frozen contributions retained) |

**Leave is self-service.** No owner approval. `DELETE /households/:id/members/me` sets `left_at = NOW()`. The personal sidebar loses the Household scope; a banner offers rejoin until grace expires.

**Last-owner block.** If the only owner attempts to leave, the API returns `409 LAST_OWNER` with a hint to transfer ownership or dissolve the household first. The household never auto-dissolves.

**Grace period** is owner-configurable on the household (`households.grace_period_days`, default **30**). Owners can shorten or end an individual member's grace period from the Members page (later milestone).

**Rejoin within grace = auto-resume.** Re-accepting the invite (or the owner re-adding the member) clears `left_at` when `purged_at IS NULL`. Prior `account_shares`, shared-budget participations, shared-goal splits, and `shared_with_household` AI threads reactivate automatically — no re-setup.

**Purge on expiry.** When `NOW() - left_at > grace_period_days`, the member's `account_shares` rows are physically deleted and `household_members.purged_at` is set. Past contributions remain in historical aggregates because aggregates are computed from transaction history, not from membership state at query time.

**Purge mechanism.** Lazy: the aggregator filters by the current time on every read, so correctness does not depend on the purge ever running. The `cmd/household-purge` runner (shipped in #161) handles disk hygiene — operators schedule it via cron / launchd / k8s CronJob.

## Rationale

- **Self-service leave** mirrors real social dynamics — needing approval to leave is hostile UX and a privacy anti-pattern (the owner shouldn't be able to trap a member's data in the household).
- **30-day default** matches typical "grace period" expectations users carry from other tools and gives a comfortable window for second thoughts without indefinite linger.
- **Lazy filtering as source of truth** means the system is always correct regardless of cron health. The runner is for storage hygiene, not behavior.
- **Last-owner block** prevents stranded households. Auto-promotion ("pick the longest-tenured contributor") was rejected because it makes ownership transfer invisible — a surprising mutation of a power relationship.
- **Historical preservation across purge** matters for a household's own books — a goal that was 60% funded shouldn't show 30% next month just because a contributor left.

## Consequences

- `household_members` carries `left_at TIMESTAMPTZ NULL` and `purged_at TIMESTAMPTZ NULL`. Soft-delete-safe uniqueness on `(household_id, user_id) WHERE purged_at IS NULL` so the same user can rejoin and (eventually) be purged repeatedly without UNIQUE collisions.
- Aggregator (ADR-0008) is the single read-side enforcement point — it knows which members are "live" and which contribute only to historical sums.
- The `cmd/household-purge` runner is non-essential for correctness — shipped in #161 as an idempotent CLI meant to be scheduled.
- Owner-configurable grace exposes a `PATCH /households/:id` (or similar) with `grace_period_days`. UI lands with the Members page (hi-fi pending).
