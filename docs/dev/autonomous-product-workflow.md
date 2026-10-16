# Autonomous Product Maturity Workflow — Cloud-first

This is the active workflow for product maturation. `docs/dev/autonomous-delivery.md` and `infra/delivery-loop/` describe the previous local M13–M15 campaign and remain as fallback. **Never run two autonomous code writers concurrently.**

## Roles and state
| Agent | Responsibility | Outcome |
| --- | --- | --- |
| Product | Verify Monarch behavior from legitimate public/accessible sources, audit actual Offbook, prioritize | Versioned behavior matrix, focused Issue and source-backed criteria |
| Developer | Implement one accepted user workflow per iteration | Feature PR with tests, no direct main writes |
| Independent QA | Verify precise PR HEAD against contract and financial invariants in isolated QA stack | PASS / FAIL / BLOCKED, evidence and regression Issues |
| Release | Confirm CI, QA and backup gates; promote approved stable candidate | Production remains distinct from main |

GitHub Issues, PRs, ADRs, and committed product docs hold state. Every invocation starts fresh. Existing code, not stale planning docs, determines what's shipped. The canonical machine-readable handoff and QA evidence format is [agent-communication-protocol.md](agent-communication-protocol.md); Agent labels are advisory, canonical implementation branch claims avoid duplicate ownership, and the independently sourced verdict status is evaluated at the exact PR head SHA.

## Provisioning
Cloud needs repository access, GitHub PR/Issue permissions, Docker Compose, Go, Node/pnpm, Playwright and documented network access. **Test these assumptions in the actual environment.** Some cloud GitHub authentication modes do not support all `gh issue`/`gh pr` operations; use supported GitHub integrations or report BLOCKED rather than injecting a broad-scoped token.

The files in `infra/cloud-routines/` are **templates only**. Creating/activating a Claude Code Cloud Routine requires owner setup, outside GitHub. They are not active merely because these files exist.

Disable the legacy local LaunchAgent via `command make delivery-uninstall` on its host before enabling Cloud delivery. OpenClaw, legacy launchd and Cloud must not be parallel code-writing schedulers.

## Per-Issue cycle
1. Product agent documents one coherent user workflow and source-backed edge cases; create/update a scoped GitHub Issue with acceptance scenarios and dependency.
2. Developer checks open PRs/branches first, resumes existing work, otherwise implements one unblocked issue in a feature branch; at most one PR per run. Run targeted and full tests as applicable.
3. Independent QA checks the **exact PR HEAD SHA** in a separate environment, runs deterministic money correctness and E2E scenarios beyond basic route smoke, then records PASS/FAIL/BLOCKED with proof. On new commits prior QA passes expire.
4. Developer fixes QA failures; QA retries independently. Required CI and a current QA PASS are mandatory for product-functional merges. Owner review remains mandatory for financial arithmetic, auth/PII, schema/data migrations, production-sync and release automation until explicit stronger protections are approved. Non-functional docs/tests may self-merge under existing delegation.
5. Merge with evidence and record resolved user job, SHA and remaining issues.

QA may run per PR and periodically against `main` on synthetic data. Discussion #199 remains the regression ledger. Never QA-certify code in the implementation session.

## Release isolation
`main` is integration, not production. Dev/Staging may follow `main` with disposable data. The Pi has separate dev and production deployments. **Owner reports that only dev currently auto-deploys; prod is deployed manually. Preserve this arrangement.** Repository capability to install a prod timer is not proof it is enabled on the Pi; inspect actual systemd state before changing any host configuration. A merged PR does not automatically mean a production rollout.

Continue manual production deployments from an explicitly chosen and verified release SHA. If a stable branch is introduced later, protect it and require owner-authorized promotion; this workflow does **not** assume one already exists. Verify restore from an off-host backup before real-data onboarding. Cloud agents get no production DB, Tailscale or live Plaid secrets. Owner handles credentials, Plaid approval and real-account linking.

## Quality metrics
Track P0/P1 bugs, tested user journeys, unexpected financial deltas, stale/failed syncs, restored backup freshness, and observed daily-use incidents. Not number of PRs closed.
