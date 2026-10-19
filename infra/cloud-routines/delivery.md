# Engineering Cloud Routine — Opus continuous delivery

Schedule: hourly at `:01` (fresh session per firing). Read AGENTS.md, docs/product/DAILY-DRIVER-V1.md, docs/product/EXPERIENCE-MATURITY-GATE.md, docs/product/MONARCH-BEHAVIOR-MATRIX.md, docs/dev/autonomous-product-workflow.md, docs/dev/agent-communication-protocol.md and current GitHub Issues/PRs. **One active objective is Goal #423: Daily Driver v1.** You are authorized to implement and merge normal code without asking the owner for code review.

**Model:** Opus 5.5 if offered in this Claude account; if unavailable, use the strongest model provisioned without adding paid API resources. Prefer implementation and regression tests over broad research/architecture prose.

### Each invocation

0. **Single-writer check (before anything else).** List open PRs whose head branch starts with `agent/`. If one exists and its latest commit or comment is **less than 60 minutes old**, another Engineering run is probably still active: post nothing and exit. If it is older, resume that PR (fix CI or merge it) and do not start new work until it is merged or explicitly blocked.
1. Check current `main` SHA, open QA findings (bug Issues and the latest checkpoint on QA Ledger #485), and the daily Product Review rollup on #423. P0/P1 QA and P2 core-journey UX/taste defects override unrelated new work. A merged feature is not necessarily mature.
2. Pick the highest-impact unblocked `agent:ready` Issue. If a simple required slice has no Issue, create a focused one from the charter first; do not stall waiting for Product. **Every PR needs an Issue.**
3. **Claim with the canonical branch `agent/issue-N`** (N = Issue number, never a PR number or matrix ID). If the harness checked out a `ccr-*` branch, create `agent/issue-N` from `origin/main` and push that instead. If `agent/issue-N` already exists, resume it; never create a second branch for the same Issue. Open the PR early with `Closes #N`.
4. Implement full vertical behavior including API/domain/UI and deterministic regression/acceptance tests. Preserve exact decimal, idempotency, reconciliation and PII ownership invariants. Multi-row/multi-table writes go in one `db.Transaction`. Migration round-trip for schema updates. Use synthetic fixtures and Plaid Sandbox only.
5. **UI changes: drive the workflow in desktop (1280) and mobile (390) browsers before merging, not in a follow-up PR.** Add or extend a Playwright spec under `acceptance/` and run it through the `QA journeys` GitHub Actions workflow (inputs `ref=<your branch>`, `suite=<spec pattern>`); download the `qa-evidence` artifact with `gh api` and look at the screenshots. Do not depend on a local Docker stack (#426). Check discoverability, hierarchy, wording, empty/loading/saved/error/retry and accessibility, and fix visible roughness. Put the evidence in the PR's "Product Experience & Polish" section. A PR body saying "no browser walkthrough yet" is not mergeable.
6. Run targeted tests and repository verification. Wait for **all required CI checks green at the exact head SHA**. On failure diagnose and fix forward; do not re-run blindly or disable CI.
7. When green, **squash-merge automatically**. Record the merge SHA and acceptance status in **one comment on the Issue** (not on #423). If GitHub branch rules prevent merge, document the exact blocker and stop.
8. If time/quota remain, go back to step 2; keep only one active code-writing branch. If quota is exhausted, stop cleanly with the PR/Issue state current.

**Hard stops:** no production deployment, no paid services/API credentials, no live bank data or production DB, no irreversible/destructive or contract-stage schema changes without explicit owner authorization, no bypass of security gates. A known P0/P1 regression must be repaired before new unrelated features.

**What counts as work:** a complete usable user journey with browser evidence, a test-backed product-quality fix, a green/merged PR, or a reproducible blocker. Never mark Goal #423 MATURE/DONE, never post an `offbook-maturity:v1` or `offbook-batch-qa:v1` comment, and never assert Monarch-equivalent UX. Repeated speculation, re-reading giant context, duplicate Issues, or tokens spent without a durable result are not progress.

### Handoff

QA runs hourly at `:35` against integrated `main` and files bug Issues plus a checkpoint on #485. Product Review runs daily: it replenishes the `agent:ready` queue, critiques the experience of recently merged journeys, and posts one rollup on #423. Fix their highest-priority findings next. Do not block ordinary PR merges on a QA label.
