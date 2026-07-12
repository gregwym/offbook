# ADR 0021: Scheduled Plaid Transaction Sync via Polling, Not Webhooks

## Status
Accepted (M14 / #363)

## Context

Through M13, Plaid transaction sync is entirely user-triggered: the initial
Link connect flow and a manual per-institution resync button (#313). Nothing
keeps data fresh between visits — the Product Goal for M14 (Milestone A) is
"new transactions appear in Offbook every day without me opening the app,"
which means Offbook needs to initiate sync on its own schedule.

Plaid's usual answer for "tell me when new data is ready" is **webhooks**:
Plaid POSTs to a URL Offbook registers when an item has new transactions.
That doesn't fit this deployment model:

- Per [ADR-0016](0016-tailscale-per-instance-deployment.md), a self-hosted
  Offbook instance is reachable only over Tailscale (`*.ts.net`), by design —
  there is no public inbound endpoint for Plaid's servers to reach. Standing
  one up (a public reverse-proxy tunnel, a relay service) would reintroduce
  exactly the attack surface the Tailscale-only model exists to avoid, for
  every self-hoster, just to receive a "go sync now" ping.
- Self-host simplicity is a recurring project value (see ADR-0020's job
  runner decision): an operator running `docker compose up` on a home
  server shouldn't also need port-forwarding, a public DNS name, or a
  webhook-relay account.

So freshness has to come from Offbook **asking** Plaid, on a schedule —
polling — rather than Plaid telling Offbook.

## Decision

**Poll.** A new job, `plaid-transaction-sync`, registered on the ADR-0020
job runner (`internal/service/jobs.Runner`, already wired in
`cmd/server/main.go`), runs once a day and calls the existing
`plaid.Service.SyncTransactions` for every active `plaid_item` across every
user on the instance. No new sync logic — this is scheduling + iteration +
error routing over the sync path #313 already built and #360 already wired
for alerting.

**Cadence and jitter:**
- **Interval: 24 hours**, `InitialDelay: 3 minutes` (after boot work — DB
  migrations, first HTTP requests — settles, matching the pattern the other
  jobs in ADR-0020 already use).
- **Jitter: a random 0–30 minute delay at the start of every run**
  (`plaidsvc.SyncScheduler.jitter`, default 30m), not just once at process
  boot. Rationale: an in-app job's *actual* daily fire time already varies
  run to run purely from process restarts (deploys, host reboots), but a
  long-lived instance that never restarts would otherwise sync at the exact
  same wall-clock minute every day indefinitely. The jitter keeps that from
  becoming a fixed, guessable pattern and spreads Plaid API load across the
  window rather than a single instant — the same rationale
  `prices.Scheduler` applies with its inter-user `pause`, one level up.
- **Inter-item pause: 5 seconds** between items within one pass (mirrors
  `prices.Scheduler.pause`) — keeps a multi-item instance inside Plaid's
  per-key rate limits rather than firing every item's sync back-to-back.

**Per-item isolation.** One user's Plaid failure — expired consent, a Plaid
outage, a malformed row — must never block another user's sync. Each item's
`SyncTransactions` call is independent; a failure is logged, counted, and the
pass continues to the next item. This mirrors `prices.Scheduler.RunOnce`'s
existing per-user isolation for the price-refresh job.

**Concurrency guard.** `SyncTransactions` itself does not check whether an
item is already mid-sync before flipping its status to `syncing` — historically
safe because only one caller (the user clicking resync) could ever invoke it
for a given item. The scheduled job breaks that assumption: a user could
click "resync" at the same moment the daily job reaches their item. To avoid
a race, `PlaidItemRepository.TryStartSync` does an atomic conditional update
(`UPDATE ... WHERE last_sync_status NOT IN ('syncing','error')`) before the
scheduler calls `SyncTransactions`; a failed CAS means "already being synced
elsewhere" and the item is skipped this pass, not retried.

**Errored items are skipped, not retried blind.** An item already sitting in
`last_sync_status = 'error'` needs a human or a re-auth flow to fix (revoked
consent, `ITEM_LOGIN_REQUIRED`) — retrying it daily would just retry-storm a
broken item and re-fire the #360 notifier every day for a condition nothing
is doing anything about. #364 (Plaid re-auth flow: Link update mode) is the
follow-up that gives a broken item a real recovery path; until it lands, the
scheduler leaves `error` items alone. This is an accepted, temporary gap:
transient errors (a one-off network blip) also get stuck until #364 or a
manual resync clears them. That trade-off is the smaller one — a stuck item
is visible (Settings already shows `last_sync_status`/`last_sync_error`) and
recoverable by a manual resync in the meantime; retry-storming a revoked
item's DLQ and alert channel is worse.

## Alternatives considered

- **Webhooks with a public relay/tunnel** (ngrok-style, or a shared
  Offbook-operated relay). Rejected: reintroduces a public attack surface
  and an extra hosted dependency for every self-hoster, exactly what
  ADR-0016 avoids. Also a second operational surface (relay uptime, relay
  auth) for a problem polling solves with zero new infrastructure.
- **User-configurable cadence.** Explicitly out of scope for #363 (see the
  issue) — one fixed daily cadence is simpler to reason about and matches
  how often a personal-finance app's data realistically changes. Revisit if
  a real need for tighter freshness emerges.
- **No jitter (fixed daily time).** Simpler, but produces a permanent
  fixed-time pattern for a long-lived process and clusters load into a
  single instant rather than spreading it. The jitter costs one `time.Duration`
  field and a `select` — cheap enough to keep.

## Consequences

- New `internal/service/plaid.SyncScheduler` (mirrors
  `prices.Scheduler`'s shape: `RunOnce`, `WithJitter`, `WithPause`) and
  `PlaidItemRepository.ListAllActive` / `.TryStartSync` — the same
  "one repo method exists for the periodic job to iterate cross-user"
  pattern `UserSettingsRepository.ListAutoRefreshUserIDs` and
  `household.RunPurge` already use. No other read path on
  `PlaidItemRepository` crosses `user_id`.
- `cmd/server/main.go` builds its own `plaid.Service` instance for the job —
  the same construction `internal/router`'s `newPlaidService` and
  `cmd/plaid-resync` already duplicate, kept separate so the job runner
  doesn't share mutable state with the HTTP-facing service.
- A failed item sync still routes through the existing `plaid.Notifier` seam
  (#360) — no new alerting path, just a new caller of the existing one.
- `last_synced_at` becomes visibly fresh in Settings/Accounts without the
  user ever clicking resync — the existing UI already renders that field
  (`SyncStatusPill`, Settings, Transactions); this job is what keeps it
  current.
- Out of scope here (per the issue): balance/holdings scheduled sync — M15
  (#369) extends this same job to cover those; re-auth recovery — #364.
