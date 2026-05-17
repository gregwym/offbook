# ADR 0009: PII Orphan-Cleanup Policy on Soft-Delete

## Status
Accepted

## Context
`pii_store` has no foreign keys to any other table by design (PII isolation, see [ADR-0003](0003-pii-isolation-table.md)). When an `account` or `transaction` is soft-deleted (`deleted_at IS NOT NULL`), nothing automatically touches its `pii_store` rows. A policy is required: do PII rows live as long as their owning entity's *row*, or as long as its *logical lifecycle*?

The choice has downstream consequences for `pii_service` API surface, soft-delete restore flows, and the GDPR/right-to-forget story.

## Decision
**Soft-delete preserves PII.** Only a hard purge (`Unscoped().Delete()`) removes PII rows.

- Soft-deleting an account or transaction is reversible (`gorm.DeletedAt` is exactly this contract). Restoring a soft-deleted account without its holder name / account number would leave the user with a half-empty record.
- PII follows the entity's **logical** lifecycle, not its row lifecycle. A row marked `deleted_at` is in the "trash" — not yet gone.
- Hard purge (when explicitly invoked, e.g. by a right-to-forget endpoint or scheduled purge job) is what removes the underlying entity rows AND their PII atomically.

## Rationale
- **UX coherence**: restore-from-trash that resurrects only half the record is worse than not restoring at all.
- **Right-to-forget remains achievable**: it requires hard purge, which was always the only acceptable answer for actual GDPR erasure (soft-delete does not satisfy "erased").
- **Minimal blast radius from a soft-delete bug**: an accidental soft-delete is recoverable. Under Policy B, an accidental soft-delete would silently destroy PII with no recovery path.
- **Aligns with the household grace period model** ([ADR-0007](0007-member-lifecycle.md)): membership uses `left_at` + `purged_at` for exactly this reason — soft state vs. terminal state are different.

## Consequences
- `pii_service` does **not** wire into account/transaction soft-delete paths. No `DeleteByEntity(entityType, entityID)` on the service.
- A future hard-purge runner (analogous to `cmd/household-purge`) MUST delete from `pii_store` in the same transaction as the entity hard-delete. Track as a follow-up issue when the first hard-purge surface lands (M3+ when `accounts.deleted_at` flows from real user actions).
- Privacy story in marketing/docs must be honest: "PII is removed when you purge the trash, not when you delete an account."
- The transitive-ownership check in `pii_service.GetAccountPII` already calls `accSvc.Get` which filters out soft-deleted accounts — so a soft-deleted account's PII is **unreachable** through the API even though the rows linger. This is the desired property: invisible by default, recoverable on restore, gone on purge.

## Alternatives Considered
- **Policy B — purge PII on soft-delete**: rejected for the UX coherence reason above. PII minimization is a real value but is better served by an explicit hard-purge UX than by aliasing it onto "delete this account".
- **Cascade via FK from `pii_store`**: rejected because it breaks the PII isolation invariant ([ADR-0003](0003-pii-isolation-table.md)) — `pii_store` is intentionally unjoinable.
- **Background sweeper that nukes orphaned PII**: rejected as adding complexity for a problem that doesn't exist under Policy A. Orphans only arise from hard-purge bugs; the right fix is to make hard-purge atomic, not to mop up after it.

## Follow-up
File a tracking issue when the first hard-purge surface lands (account permanent-delete UX, scheduled trash auto-purge, or right-to-forget endpoint) to ensure `pii_repo.DeleteAll(entityType, entityID)` is called inside the same transaction as the entity hard-delete.
