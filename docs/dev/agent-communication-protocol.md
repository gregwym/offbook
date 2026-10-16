# Offbook Agent Communication Protocol v2 — engineering-first

Three independent roles communicate **asynchronously** through GitHub Issues, PRs and committed product docs. No shared session context, master agent, OpenClaw coordinator or dedicated VPS.

**Source of truth:** [Daily Driver v1 charter](../product/DAILY-DRIVER-V1.md) / [Issue #423](https://github.com/gregwym/offbook/issues/423). This is one concrete goal to drive continuously until acceptance, not an invitation to infinitely research Monarch features.

## Labels (advisory)

Bootstrap with `bash scripts/agent-protocol/setup-labels.sh --apply` (explicitly; default is dry-run). Product: `agent:research`, `agent:ready`. Developer: `agent:claimed`, `agent:blocked`. QA: `agent:qa-failed` (bug/QA batch needing repair), `agent:qa-passed` (informational batch state). `agent:qa-needed` is optional for targeted requests only; **not set on every PR**. Labels never authorize a merge.

## Issues / PRs as durable work

- Product Issue: measurable Product Goal, current code gap, sourced observable behavior where needed, financial/privacy invariants, tests, dependencies, out-of-scope. A ready Issue should be implementable without extra product research.
- Engineering PR: `Closes #N`, concrete user-visible behavior, regression tests, exact CI and merge evidence, migration/rollback notes. Use the standardized PR template.
- QA Bug Issue: exact tested `main` SHA, reproduction, expected/actual, severity P0/P1/P2/P3, regression proposed. QA's findings outrank future features when P0/P1.
- Progress: each completed PR updates its parent Issue/goal with merge SHA and next acceptance gap. No extra orchestration database or paid event broker.

## Single-writer Issue claiming

Issue `#N` uses canonical implementation ref `agent/issue-N`. Git branch creation is a unique initial claim, whereas labels are not atomic. Before creation, search PRs and branches for earlier work. If the branch exists with an open PR, resume **only if the writer is your own paused workflow**. A competing simultaneous run must exit. A stale branch with no PR is blocked for deliberate recovery, never force-push/reassign silently.

Begin with a single Engineering Routine scheduler. One active PR/branch at a time; CI-green merges clear the queue. No artificial weekly PR quota. Resume interrupted PRs before opening new ones.

## CI gate vs independent QA

**Every PR:** required deterministic CI on the **exact head SHA** (Go tests with Postgres, lint, migrations, schema, frontend tests/build, contract checks, Playwright baseline plus targeted new tests). Developer must write/adjust tests for the behavior being changed and cannot disable failing required checks to merge.

**After batches of merges:** a separate QA Session checks integrated `main` since the previous QA checkpoint. Default twice per week, or optionally after about eight merges. Independent QA checks financial truth, sync/import idempotency, mobile flows and error states from user perspective rather than retesting every PR. It creates actionable Bug Issues and updates the QA Ledger/goal. This is **not** a required status check for each PR, and separate GitHub identities are not required: separate sessions/workspaces suffice for independent functional testing.

**Release:** no known P0/P1, full acceptance scenarios pass, final SHA has an independent batch QA PASS and verified database restore evidence. CI-green `main` is an integration candidate, not automatically production-ready.

### Batch QA checkpoint format

Publish a comment on Goal Issue #423, and when supported also update Discussion #199:

```text
<!-- offbook-batch-qa:v1 {"from_sha":"<40-hex>","to_sha":"<40-hex>","result":"PASS|FAIL|BLOCKED","run_url":"https://...","checked":["money-invariants","mobile-smoke"],"issues":[423]} -->
```

The SHA range is from last accepted QA checkpoint (or a documented initial baseline) through a pinned new `main` SHA. Verify ancestry and never claim a newer commit than actually tested. PASS requires evidence links and specific coverage; FAIL/BLOCKED records defects and the exact tested range. New commits after the checkpoint are *unreviewed*, not retroactively passed. A later QA run can cover multiple preceding merges; no need to certify each PR individually.

## Merge policy

User has explicitly delegated normal code review and `main` integration. Engineering may squash-merge when all required CI checks are green, issue acceptance satisfied and no known P0/P1 in the touched area. This includes correct financial implementations with new deterministic regression tests and safe reversible schema additions. Stop rather than auto-merge destructive/non-backward-compatible migrations, security/PII bypasses, known loss/corruption bugs or any change that requires live production access. Such cases need explicit owner approval **for the operation or risk decision, not line-by-line code review**.

Merging to `main` makes the owner's Pi dev eligible for its existing auto-deploy; Pi prod stays manual and is not part of Cloud access.

## Routine trigger policy

Engineering: scheduled approximately every three hours, one active writer. Product: as-needed issue queue replenishment when fewer than three accepted Daily Driver issues remain (check daily/weekly but no-op cheaply otherwise). QA: twice-weekly scheduled integrated main audit, optionally after a sufficiently large merge batch. GitHub Issue comments/labels are persistent communication, not assumed to wake a Cloud Routine directly.

## Product maturity graduation, not feature-count closure

After a series of CI-green PR merges, the Product Goal #423 remains open until independent QA + Product Experience Critic replay and score all six journeys on the exact candidate SHA. The [experience maturity rubric](../product/EXPERIENCE-MATURITY-GATE.md) requires visually evidenced desktop/mobile UX, dated Monarch behavior comparisons, recovery, no P0/P1/core P2 and documented stability. `.github/workflows/goal-maturity-guard.yml` automatically reopens #423 if closed as completed without a structured `offbook-maturity:v1` certificate matching current `main`. It verifies evidence completeness, not subjective taste itself.

## Pilot

Run preflight in disposable Cloud first; then exercise **one simple code PR to green CI and auto-merge into `main`**, followed by one batch QA session on that commit, creating Bug Issues if needed. Confirm the old local loop is disabled before activating the Cloud writer. Only then enable recurring schedules. No distinct QA GitHub identity, separate bot, full-time orchestration service, or routine owner code review is needed.
