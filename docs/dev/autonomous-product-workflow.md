# Autonomous Product Workflow — Delivery-first, batched QA

This is the **current operating model** for [Daily Driver v1](../product/DAILY-DRIVER-V1.md) ([Issue #423](https://github.com/gregwym/offbook/issues/423)). This goal supersedes the legacy M13–M15 delivery queue. GitHub Issues/PRs are persistent work state; Cloud sessions do not share memory.

## Resource policy

Owner has delegated engineering implementation and integration merges; **do not ask for human code review**. Opus 5.5 is the preferred coding model if available. Use subscription capacity to complete valuable functionality, not to run three parallel thinking agents all day. Exhausted quota is normal; exhausted quota without a reproducible change, test, merged PR or blocker explanation is not.

**Default cadence after preflight:** Engineering Cloud Routine every ~3 hours (or the most frequent schedule actually supported), one active writer, continuous delivery while accepted work exists. One PR at a time, but no weekly PR count cap; an invocation may proceed to another issue after a completed merge if remaining time/quota permit. Pause rather than overlap another run on the same branch. No VPS, Mac mini orchestration or 24/7 owner MBP requirement.

Product research is **demand-driven**: replenish the issue queue when fewer than three ready, independent Daily Driver issues remain, or an edge-case decision blocks engineering. Avoid paid/routine Monarch research merely to create backlog volume.

Independent QA **and independent Product Experience Critic** are batched, not on each push or PR: twice weekly or ~8 merges against a pinned `main` SHA range. QA must follow [Experience Maturity Gate](../product/EXPERIENCE-MATURITY-GATE.md), evaluating full journeys, UI polish, error recovery, mobile, and source-backed Monarch behaviors with actual browser evidence. QA may perform targeted high-risk review earlier for known P0/P1 suspicion, but is **not a routine merge prerequisite**.

## Autonomous cycle

1. Read [Daily Driver charter](../product/DAILY-DRIVER-V1.md), [handoff protocol](agent-communication-protocol.md), open Issues/PRs and code to determine true implementation state. Prioritize open QA P0/P1 **and P2 core-journey UX defects** over unrelated new features.
2. Claim one accepted issue via canonical `agent/issue-N` branch. Only one Engineering writer at a time; open PR early so a restarted session can resume. Prefer vertical slices with explicit user-visible acceptance and deterministic regression tests.
3. Implement and validate with synthetic/Plaid Sandbox data only. Run relevant local checks; `main` merges require **all required GitHub CI checks green** for the exact PR SHA. Fix red checks, don't re-run them blindly without diagnosing failure.
4. **Squash-merge automatically** once required CI is green and the branch protection permits it. Update Issue with the merged SHA and acceptance achieved. Do not ask owner to review code; an unresolved permission/branch protection constraint is a documented infrastructure blocker, not a reason to claim a merge succeeded.
5. Repeat on next accepted issue when time/quota remain; otherwise end cleanly with next queue item and blockers. Never merge directly into prod or access its secrets.
6. QA independently reviews an integrated `main` commit range **in batches**, tests critical browser/API/financial invariants, files Bug Issues (P0/P1 plus P2 core UX first), and separately records technical PASS/FAIL/BLOCKED and experience MATURE/NOT MATURE/BLOCKED, a screenshot-backed scorecard and last QAed main SHA. Technical PASS cannot close the maturity goal.
7. Developer's next iteration prioritizes batch-QA P0/P1 issues and regression tests. Only when all target flows actually work at desktop/mobile size, final independent Product Experience audit scores >=3 per applicable dimension with no open core P2, financial reliability/repeat ingestion and backup restore are verified, may QA propose Daily Driver v1 MATURE on the exact candidate SHA. Production release still belongs to owner.

**Final milestone closure:** Issue #423 has an automated `issues.closed` guard that reopens any completed goal lacking a complete independent `offbook-maturity:v1` certificate for the current main SHA, with the full browser-journey UX scores (all >=3), synthetic soak and backup restore evidence. This prevents an Engineer's completed PR checkboxes from defining Product Mature. See [Experience Maturity Gate](../product/EXPERIENCE-MATURITY-GATE.md).

## Safety and stop conditions

Allowed without code review: feature fixes, test improvements, compatible schema additions with migration round-trip, ordinary refactors, normal `main` merge on green CI. **Not allowed without explicit user authorization:** destructive/irreversible migration or production data rewriting, production deploy/release, onboarding real financial institutions, broad financial-secret access, paid external resources, or bypassing security controls/tests.

Any observed money-integrity or PII leak regression must become a P0/P1 Issue and block release. If a regression is known to affect `main`, fix it before delivering more unrelated features. The developer is still accountable for targeted testing at implementation time; batching AI QA does not mean merging untested changes.

## Cloud and host setup

Cloud environment must verify actual GitHub PR/Issue API rights, Go, Node, pnpm, Docker Compose and Playwright. `infra/cloud-routines/` contains prompts, not activated Routines. Turn off the old Mac LaunchAgent before enabling Cloud Engineering; never run OpenClaw, local launchd and Cloud as multiple concurrent code writers.

Pi **dev** follows `main` automatically as currently configured; Pi **prod** is manually deployed. Do not alter timer behavior or touch live bank data in Cloud. Preserve backups and restore checks. No server/VPS needed.

## Measuring useful progress

Daily Driver scenario coverage and P0/P1 defects matter more than PR count. Each session records changes and tests actually completed, Issue/PR/merge URLs and next target. Every QA batch records `from_sha`, `to_sha`, tested flows, findings and new confirmed behavior.
