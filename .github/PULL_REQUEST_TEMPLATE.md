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
- [ ] Exact HEAD SHA identified for independent QA

**Independent QA:** Not requested / BLOCKED / FAIL / PASS  
**QA evidence link + SHA:**  
<!-- A checkmark is not a substitute for independent QA in a separate session. New commits invalidate old QA PASS. -->

## Schema / Release Risk
<!-- Migration expand/migrate/contract plan, backup or rollback notes, or "not applicable". -->
- [ ] No production data or secrets included in code, logs, screenshots or traces
- [ ] This PR does **not** automatically authorize deployment to the Pi's production instance

## Review Gate
<!-- For financial arithmetic, auth/PII, migrations, production sync or release automation: owner review is mandatory until stronger gates are formally adopted. -->
