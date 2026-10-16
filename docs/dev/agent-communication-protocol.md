# Offbook Agent Communication Protocol v1

## Purpose and authority

Product, Engineering, and Independent QA are separate fresh Claude Code Cloud sessions. **GitHub Issues + PRs are the durable inbox and work record**; there is no shared agent memory and no central OpenClaw process. See [Product Contract](../product/PRODUCT-CONTRACT.md) and [Cloud workflow](autonomous-product-workflow.md).

This document specifies the handoff interface. The scripts, workflows and prompts implement parts of it; **Cloud Routines and a trusted QA identity must still be configured by the owner**. Do not report the end-to-end loop as active until the pilot has passed.

## Status labels (advisory, not a lock or security boundary)

Create the labels using `bash scripts/agent-protocol/setup-labels.sh --apply` (requires authenticated `gh` and write rights); default invocation is dry-run.

| Label | Used on | Meaning |
| --- | --- | --- |
| `agent:research` | Issue | Product evidence / decision incomplete |
| `agent:ready` | Issue | Sourced behavior contract + testable acceptance ready |
| `agent:claimed` | Issue | One engineering candidate is being worked |
| `agent:blocked` | Issue or PR | Explicit environmental / decision blocker |
| `agent:qa-needed` | PR | Candidate awaits an independent QA run |
| `agent:qa-failed` | PR | QA reported FAIL or BLOCKED for a tested SHA |
| `agent:qa-passed` | PR | Informational; **never proof of acceptance** |
| `agent:qa-verdict` | PR | Event nudge to refresh the GitHub QA gate after a verdict comment |

Product chooses `agent:research` or `agent:ready`. Engineering may add `agent:claimed` and `agent:qa-needed`. QA posts verdict and sets the corresponding QA status, then **toggles** `agent:qa-verdict` (remove if present, add again) to recalculate the check. The final nudge is necessary because `issue_comment` GitHub Actions runs do not produce ordinary required PR status checks.

## Issue as the inter-agent API

An accepted Product Issue includes: `Product Goal`, evidence with URL/date/confidence, `Behavior Specification`, `Edge Cases`, `Financial / Authorization Invariants`, `Acceptance Scenarios`, `Dependencies`, `Out of Scope`. Use [agent product issue template](../../.github/ISSUE_TEMPLATE/agent-product.md). Scope one usable vertical slice per Issue.

QA creates a separate bug Issue using [agent bug template](../../.github/ISSUE_TEMPLATE/agent-bug.md), with reproducible steps, expected/actual state, offending **commit SHA**, sanitized evidence, and a link to the parent Issue and PR. A bug should be fixed ahead of starting a new Feature.

Comments clarify decisions; changes in product semantics must be reflected in the Issue body or versioned Product Contract, not left buried in chat comments.

## Claim without duplicate work

GitHub Labels and `issue edit` are **not atomic**. For one Issue `#123`, the *canonical implementation branch* is `agent/issue-123`. Creating this Git reference is the claim; GitHub accepts only one creation for a given name.

1. Engineering checks whether an open PR already references `#123` or uses `agent/issue-123`. If yes, **resume or report blocked**, never start a second implementation.
2. Otherwise check whether `refs/heads/agent/issue-123` exists. If absent, create it **once** from current `main`, without force.
3. Only the routine that successfully created it may start implementation in this invocation. A competing run that sees it already exists exits and does not push.
4. Create the draft PR early, before expensive work. Associate `Closes #123`, mark `agent:claimed`, and record the Cloud run link in PR/Issue.
5. A stale branch without an open PR is **BLOCKED** until owner-authorized recovery; do not repurpose or delete its branch automatically.

**Start with only one Engineering Routine execution at a time.** Branch creation avoids duplicate claims but is not a distributed lease and does not make concurrent writes to a shared branch safe. QA never writes to the implementation branch. Product only writes its separate documentation branch.

## QA verdict contract

QA uses an isolated environment and a **separate agent session**. The PR's comment must contain this exact one-line machine-readable marker, plus human-readable test evidence:

```text
<!-- offbook-qa-verdict:v1 {"head_sha":"0123456789abcdef0123456789abcdef01234567","result":"PASS","run_url":"https://example.com/qa-run/123","checks":["transaction-refund","transfer-idempotency"]} -->
```

Allowed results are `PASS`, `FAIL`, `BLOCKED`. An empty tests array or missing HTTPS evidence link cannot support PASS. The result always names a **40-character PR head SHA**. A new push invalidates the earlier result. If QA later finds a failure on the same head, the latest trusted verdict supersedes a prior PASS.

`scripts/agent-protocol/qa-verdict.mjs` interprets the records and is covered by Node unit tests. `.github/workflows/agent-qa-gate.yml` evaluates them for canonical `agent/issue-<n>` PRs, using only the repository's trusted default-branch script, not PR-authored code.

**Activation and identity prerequisites:** the validator is initially **opted out** so it cannot create surprise red status checks on existing PRs. Set GitHub repository variable `OFFBOOK_AGENT_QA_ENABLED=true` when ready to conduct the pilot; leave it unset/false otherwise. Also set GitHub repository variable `OFFBOOK_QA_ACTORS` to a comma-separated allowlist of QA GitHub login(s) **different from the PR author**. A separate Cloud session with the **same** GitHub identity cannot provide cryptographically independent evidence, and the gate intentionally fails closed. A dedicated QA bot/app is recommended; do not weaken the check to make a same-user pilot green. The gate does not prove the tests themselves were run; it verifies trustworthy provenance, structure and SHA. GitHub CI and independent QA practice provide the rest.

The PR label is *not* evidence, and manually editing a PR body cannot generate QA approval.

## Merge and release

- A code-writing Agent does not self-certify. For an agent branch: required repo CI green, latest trusted QA verdict PASS for exact head SHA, and (for finance/auth/PII/migrations/production-sync/release code) owner approval.
- Set `OFFBOOK_AGENT_QA_ENABLED=true` to run the pilot and trigger a fresh PR `labeled` event, then enable the `offbook/independent-qa` commit status as required in GitHub branch protection **only after the separate QA actor is configured and the pilot passes**. Merely committing the workflow does not change branch protection. For non-agent branches this check reports a no-op; they remain governed by the existing review policy. Labels or branch names alone cannot be a universal anti-bypass security guarantee.
- The Pi **dev** deployment continues following `main` automatically; owner reports **prod** is manually deployed. No changes to Pi deployment policy are made here.
- Never send real bank information, Plaid production secrets, or live DB state to Cloud QA.

## First autonomous pilot (one loop, no schedule)

1. **Cloud environment:** run `bash scripts/agent-protocol/cloud-preflight.sh --quick` and then, in a disposable Cloud clone, `--full`. The full test boots the existing isolated QA Compose stack and runs baseline Playwright smoke. Report command outputs and any blockers.
2. **Bootstrap:** owner applies labels, configures a separate QA GitHub actor and sets `OFFBOOK_AGENT_QA_ENABLED=true` only on entering the pilot; create Cloud Routines based on `infra/cloud-routines/` without enabling overlapping scheduled writers. Disable legacy Mac LaunchAgent if it is still active.
3. **Product:** create one small, low-risk, real Daily Driver Issue based on documented behavior. Do not create dozens of issues up front.
4. **Engineering:** claim with deterministic branch and open a PR implementing the Issue.
5. **QA:** validate PR head SHA independently, post verdict and toggle `agent:qa-verdict`.
6. **Gate:** verify a correct PASS turns the PR check green; a new commit makes it red; a same-author or untrusted PASS is rejected; a later FAIL supersedes PASS.
7. **Merge:** only then enable ongoing schedules. If a step fails, keep it stopped and file an issue instead of silently bypassing the safeguard.

## Routine trigger policy

Product: daily/weekly scheduled scan, one research slice. Engineering: scheduled scan, one Issue and one PR maximum; block if an in-flight candidate is owned by another run. QA: PR `opened`/`synchronize` events and periodic reconciliation scan; verify most recent head and skip duplicate verdicts. GitHub Issues and comments are durable communication but are not assumed to be directly supported Claude Code Cloud Routine triggers. No VPS or live Pi access is needed.
