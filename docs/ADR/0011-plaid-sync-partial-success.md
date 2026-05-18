# ADR 0011: Plaid `/transactions/sync` — Per-Row DLQ, Cursor Advances on Partial Failure

## Status
Accepted

## Context
Until this ADR, `service/plaid.SyncTransactions` was all-or-nothing per item: a single bad row (decimal overflow, malformed date, validation error, transient DB error on one INSERT) rolled back the entire transaction and left the Plaid sync cursor stuck on the prior page. The next sync re-fetched the same delta, hit the same bad row, and rolled back again. For a 10k-row initial pull with one poison row, 9,999 good rows were lost on every attempt and the user had no path forward short of manual SQL.

ADR-0008-style "fail closed" is appropriate for cross-user reads (privacy is the higher-order property). For ingestion, it's the wrong tradeoff: data freshness matters and the cost of dropping one well-formed row to preserve "perfect atomicity" is high.

Plaid's own model treats `/transactions/sync` as a delta stream — the cursor is the only thing that links one sync to the next. Once a row appears in an `added` page and we acknowledge the cursor past that page, Plaid will not re-send it. That makes "advance the cursor but lose the row" especially costly: the row is gone forever unless we keep it ourselves.

## Decision
1. **Per-row savepoints.** Each `added` / `modified` row is processed inside its own Postgres `SAVEPOINT`. On row-level error we `ROLLBACK TO SAVEPOINT` and continue; on success the savepoint is released implicitly when the outer transaction commits.
2. **`plaid_sync_errors` DLQ table.** Failed rows are persisted in the same outer transaction with the full Plaid payload (`raw_payload JSONB`), an error code, and an error message. Hard-delete only; "dismissed" is recorded via `resolution`, not deletion.
3. **Cursor always advances.** Once Phase 1 has drained all pages and Phase 2 has processed every row (success or DLQ), `plaid_items.cursor` is updated to Plaid's `next_cursor`. The DLQ row preserves enough information to replay the original payload, so the cost of moving the cursor past a failed row is bounded.
4. **`last_sync_status='ok_with_errors'`** when any rows DLQ'd. Distinguishes partial success from a clean run in the UI without burying the signal in `last_sync_error` text.
5. **Owner-driven retry/dismiss.** `POST /plaid/errors/:id/retry` re-runs the row through the same mapping path inside a new transaction. `POST /plaid/errors/:id/dismiss` marks resolved without replay. No automatic retry — the failure modes (mapping bugs, decimal overflow, missing accounts) are all fixed-by-human, not fixed-by-waiting.
6. **`removed` rows do NOT go to DLQ.** They carry no payload and soft-delete is idempotent — a real DB error on a `removed` row indicates something systemic and should abort the sync.

## Rationale
- **Savepoints over per-row transactions.** A single outer transaction lets the cursor advance atomically with the row writes and the DLQ rows. Per-row transactions would commit good rows but require careful ordering to avoid a window where the cursor advances without the DLQ row landing.
- **Raw payload in JSONB, not parsed columns.** The whole point of the DLQ is to preserve enough state to replay later. Parsing into typed columns would lose any field we didn't think to model, and the failure modes most worth replaying (decimal overflow, new Plaid fields we haven't mapped yet) are exactly the ones where a parsed schema would lose information.
- **Cursor advances, even with N failures.** The alternative — block the cursor until the DLQ is empty — sounds safer but creates an operationally awful state. The owner can't take action without manually inspecting the DLQ. Worse, it means a single permanently-broken row blocks every future sync forever. Plaid's own model expects the consumer to advance the cursor; the DLQ is our local accommodation for partial success.
- **Status enum extended, not overloaded.** Existing `ok`/`error` callers continue to work; `ok_with_errors` is additive. The UI keys badge visibility off the `unresolved_sync_errors` count, not the status string, so the status is purely informational.
- **No automatic retry.** Real failure modes (decimal overflow on crypto amounts, malformed date parsing) need code or data fixes, not retry. Auto-retry would mask the signal.

## Consequences
- `service/plaid.SyncTransactions` now succeeds even when individual rows fail. Callers see a non-zero `result.Failed` and can prompt the user to inspect the DLQ.
- `last_sync_status` now has five values: `never`, `syncing`, `ok`, `ok_with_errors`, `error`. The check constraint enforces the enum.
- Operators must inspect the DLQ to act on broken rows. Settings → Linked Institutions surfaces a `⚠️ N` badge and a modal listing each unresolved row with the raw payload, error code, and Retry / Dismiss controls.
- `plaid_sync_errors` is a hard-delete table — no `deleted_at`. Right-to-forget on user account deletion must include this table.
- Multi-tenant: every read and resolve goes through `user_id`; the indexed predicate `WHERE resolved_at IS NULL` keeps the badge-count query cheap as the DLQ grows.

## Alternatives Considered
- **Roll back the whole sync on any row failure (status quo).** Rejected — see Context. The operational cost is too high.
- **Per-row transactions instead of savepoints.** Workable, but loses atomicity with the cursor advance. Either the cursor commits separately (window where good rows are written but cursor stale on crash) or each row + cursor is its own tx (10k round-trips for a historical pull).
- **Skip DLQ — log row failures to stderr only.** Rejected. Logs are not actionable in self-hosted deployments. The owner needs a UI surface with the raw payload to file a bug or apply a workaround.
- **Auto-retry with exponential backoff.** Rejected for v1. None of the actual failure modes self-resolve; the DLQ row would just churn. Reconsidered if a class of transient failure emerges.
- **DLQ entries deduped on `plaid_transaction_id`.** Rejected for v1 — if the same row fails twice across syncs (e.g. the user retries after a partial fix), having two rows is acceptable signal. Revisit if DLQ volume turns out to be noisy.

## Follow-up
- Optional global "all DLQ rows across all items" view if any operator hits the noisy case.
- Per-error-code metric in any future operator dashboard.
- If a class of automatically-retryable error emerges (e.g. a transient PG connection blip), add a `next_retry_at` column and a small worker.
