## Product Goal
<!-- The user-visible outcome, not just a backend task. -->

## Behavior Contract
<!-- Link an Offbook product requirement, accepted ADR, or source-backed behavior spec. Clearly mark unverified Monarch behavior. -->

## User Journeys & Edge Cases
<!-- Entry state → action → observable result. Include errors, retries and empty states. -->

## Financial / Security Invariants
<!-- Explicitly state money, reconciliation, idempotency, privacy, PII and synchronization impact, or "none". -->

## Verification
- [ ] Required repository CI checks passed
- [ ] Automated unit/integration tests updated as appropriate
- [ ] Relevant UI/E2E workflow acceptance checked (or documented why not)
- [ ] Confirmed the PR has no known P0/P1 regression in the changed area

**QA batch / requested targeted QA:** (optional; integrated `main` is audited separately)  
<!-- Independent AI QA is batched and is NOT a per-PR merge prerequisite. Existing deterministic CI remains mandatory. -->

## Schema / Release Risk
<!-- Migration expand/migrate/contract plan, backup or rollback notes, or "not applicable". -->
- [ ] No production data or secrets included in code, logs, screenshots or traces
- [ ] This PR does **not** automatically authorize deployment to the Pi's production instance

## Review Gate
<!-- Ordinary code changes, including tested financial logic and backward-compatible migrations, may auto-merge after required CI; owner approval is only for live production actions, paid credentials/services, destructive/irreversible operations, or explicit risk decisions. -->
