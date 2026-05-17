# ADR 0006: Multi-Tenant Model — Instance, Users, Households, Scopes

## Status
Accepted

## Context
M0–M2 modeled Offbook as a single-user app. A self-hosted family instance needs to host multiple users and let some of them share a financial view. Designs (`docs/designs/App Hierarchy v4.html`, v4 · lifecycle locked) settled the model: one instance, many users, optional household membership, and a binary scope picker. This ADR ratifies it.

## Decision

**Tenancy hierarchy:** one Offbook **instance** → many **users** → each user is a member of **at most one household** at any time. Many households can coexist on the same instance without seeing each other.

**Scopes:** every signed-in user sees one of two route lists at a time:
- **Personal scope** (`👤`) — the user's own book. The original 9 routes.
- **Household scope** (`🏠`) — the 6 household routes. Only visible if the user is a member of a household.

The two route lists are **mutually exclusive** in the sidebar — switching scope replaces the navigation entirely. Default scope at login is Household if the user has one, else Personal. The user's last choice persists.

**Visibility is per-account, not per-transaction.** Sharing an account shares all of its transactions. Three visibility levels per account: `private` (default), `balance_only`, `balance_and_txns`. No row in `account_shares` = `private`.

**Roles within a household:** `owner`, `contributor`, `view_only`. Only owners can invite, set grace period, and dissolve.

**Auth & signup modes:** the first user to sign up on a fresh instance becomes the admin and picks the signup mode in the same step:
- `local_multi_tenant` — anyone hitting the box can self-create an account.
- `invite_only` — admin must issue an invite token first. **Default for new instances.**

## Rationale

- **Why at most one household per user?** The hi-fi scope picker is a binary toggle. Multi-household membership would require a 3+ way picker and ambiguous defaults; the product spec explicitly closes this door.
- **Why per-account visibility (not per-transaction)?** Per-transaction sharing forces the user to make hundreds of decisions and creates a constant leak risk on import. Per-account makes the unit of trust legible: "this account is shared with my household, balance + transactions."
- **Why three visibility levels (not two)?** `balance_only` enables joint net-worth math without surfacing transaction history — a recurring real-world request ("she can see my 401k is healthy but not what I spend on"). Cost is one extra chip state. Worth it.
- **Why mutually exclusive route lists (not a unified sidebar)?** Avoids ambient context bleed. When you're acting on shared data you should *know* you are. The scope picker is the only mode signal users need.
- **Why `invite_only` as default?** Even on a home box, default-open signup means a misconfigured port-forward becomes an account-takeover surface. `local_multi_tenant` is opt-in.

## Consequences

- Every domain table (accounts, transactions, budgets, savings_goals, investments) gains a `user_id NOT NULL FK`. All M2-era endpoints become user-scoped, deriving `user_id` from the session.
- The aggregator (see ADR-0008) is the only place that crosses user boundaries; it does so only within a household and only via opt-in shares.
- The scope picker lives in app-shell state, persisted per user (`users.last_scope`) and synced via `GET/PATCH /me/scope`.
- First-boot UX adds a `/setup/admin` step. After that, the box behaves per the chosen signup mode.
- "Default user" / "single tenant" code paths are not added — the service has no production data to inherit, so the cutover is clean.
