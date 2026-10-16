# Engineering Cloud Routine — Opus continuous delivery

Read AGENTS.md, docs/product/DAILY-DRIVER-V1.md, docs/dev/autonomous-product-workflow.md, docs/dev/agent-communication-protocol.md and current GitHub Issues/PRs. **One active objective is Goal #423: Daily Driver v1.** You are authorized to implement and merge normal code without asking the owner for code review.

**Model:** Opus 5.5 if offered in this Claude account; if unavailable, use the strongest model provisioned without adding paid API resources. Prefer implementation and regression tests over broad research/architecture prose.

### Each invocation

1. Check current `main` SHA, latest QA batch findings, all open PRs, existing `agent/issue-N` branches, and goal #423. P0/P1 QA bugs override new feature work. If a previous PR is in progress, resume it; never create a duplicate.
2. Select the highest-impact unblocked Goal #423 slice. Prefer existing #374, #375, #376 and specific financial correctness/transaction review gaps after verifying actual code. If a simple required slice has no Issue, create a focused one from the charter; do not stall waiting for Product. Claim `agent/issue-N` uniquely and open PR.
3. Implement full vertical behavior including API/domain/UI and deterministic regression/acceptance tests. Use synthetic fixtures and Plaid Sandbox only. Preserve exact decimal, idempotency, reconciliation and PII ownership invariants; migration round-trip for schema updates.
4. Run targeted tests and repository verification. Publish PR and wait for **all required CI checks to finish green at the exact head SHA**. On failing checks diagnose and fix forward. Do not relaunch identical failing work repeatedly, do not disable CI to unblock a merge.
5. When green, **squash-merge the PR to `main` automatically** and record merge SHA/Issue/goal outcome. No per-PR AI QA requirement and **no owner code review**. If GitHub branch rules technically prevent merge, document exact blocker and stop rather than claim success.
6. If further actionable tasks and time/quota remain, select another issue; keep only one active code-writing branch. If quota is exhausted, stop cleanly with next Issue/PR and tests in GitHub.

**Hard stops:** no production deployment, no paid services/API credentials, no live bank data or production DB, no irreversible/destructive or contract-stage schema changes without explicit owner authorization, no bypass of security gates. Known P0/P1 regression must be repaired before new unrelated features.

**What counts as work:** user-visible scenario newly passing, test-backed fix, green/merged PR, or reproducible blocker. Repeated speculation, re-reading giant context, generating duplicate Issues, or tokens spent without a durable result are not progress.

### Handoff

Product maintains a small queue but is not on the critical path. Independent QA sweeps merged `main` twice weekly (or batch threshold); it files bugs and Goal #423 checkpoint comments. You fix its highest-priority findings next. Do not request a distinct QA account or block ordinary PR merges on a `qa:passed` label.
