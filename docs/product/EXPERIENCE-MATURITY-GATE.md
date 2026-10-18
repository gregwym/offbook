# Experience & Maturity Gate — Monarch-caliber core workflows

**Applies to:** [Daily Driver v1](DAILY-DRIVER-V1.md), [Goal #423](https://github.com/gregwym/offbook/issues/423), and subsequent Offbook maturity releases.

**Principle: a merged feature is not a mature feature.** CI-green merges are allowed continuously, but neither the Product Agent nor Engineering may mark this goal **MATURE/DONE** until independent end-to-end *experience* and reliability evidence meets this contract. Implemented → Usable → Mature are distinct states.

## What Monarch-caliber means (and what it does not mean)

- **Comparable core-workflow quality:** for each in-scope journey, Offbook should be accurate, discoverable, low-friction, consistently explained, visually coherent and robust across desktop/mobile—at a standard appropriate for a mature personal-finance product such as Monarch Money.
- **Do not claim unverified full Monarch parity:** only call an individual behavior `verified-equivalent` when observed behavior, source/observation date, screenshots or task evidence and edge cases support it. If Monarch's current UI is inaccessible, compare against official help/release documentation, mark UX comparison as `unverified`, and never fabricate findings.
- Parity **coverage** and parity **quality** are separate. A polished subset is still a subset; the overall product must not claim *complete* Monarch feature parity when key user jobs remain unimplemented. The v1 single-user goal is one scoped stage, not the end of the broader maturity roadmap.
- Favor user outcomes over pixel imitation. Copy no proprietary content or code; do not require Monarch login credentials in Cloud agents.

## Build a reference corpus once, maintain only when affected

The Product Agent populates `docs/product/MONARCH-BEHAVIOR-MATRIX.md` with, for each user journey:

`journey ID | user intent | Monarch official link/observed UI + observation date | evidence confidence | Offbook behavior | parity: missing / partial / verified-equivalent / unverified | UX discrepancy | related Issue | automated checks | QA evidence`.

Start with these *documented example interactions* (not asserted visual/UI parity):

1. **Transactions, rules & review:** Monarch's rules support previews, category/merchant/tag changes, review status, ordering, and split rules. Evaluate Offbook's equivalent *complete user journey*, not whether a rule API exists. [Monarch official rules guide](https://help.monarchmoney.com/hc/en-us/articles/360048393372-Transaction-rules).
2. **Hide vs ledger:** Monarch hidden transactions remain visible in the full transaction list but are omitted from cash-flow/budget calculations. Evaluate account balances, review, category/report consistency and undo together. [Official hide behavior](https://help.monarchmoney.com/hc/en-us/articles/4405041904916-Hide-Transactions).
3. **Pending→posted edits:** Monarch explicitly offers a preference for handling pending edits because bank posting can overwrite them. Offbook must make its own policy clear, preserve user expectations and test retries/replacement. [Official September 2026 tips](https://help.monarch.com/hc/en-us/articles/28953573066260-Tips-Tricks-for-Using-Monarch). **Note:** the older [May 2025 rules guide](https://help.monarchmoney.com/hc/en-us/articles/360048393372-Transaction-rules) and newer tips differ on whether rules run on pending transactions; verify actual observed behavior, document the chosen Offbook policy, and test it.
4. **Transfers and card payments:** Monarch treats them separately from expense/income budget reporting. Cross-check Offbook's transaction, report, budget and account totals. [Official budget guide](https://help.monarchmoney.com/hc/en-us/articles/360048883631-Budget).
5. **Budget rollover:** Monarch rollover is visible and adjustable as part of budgeting. Evaluate Offbook's monthly carry-forward semantics as a complete feature if in release scope; otherwise classify clearly as missing/deferred, not equivalent. [Official rollover guide](https://help.monarchmoney.com/hc/en-us/articles/4411119762196-Rollover-budget-feature).

Reference material is **behavioral evidence**, not proof of Monarch's current screen layout or performance. Record the fetched/observed date (do not imply every source is up to date), and request legitimate UI access only when a visual parity assertion truly depends on it.

## Review real journeys, not isolated screens

A batch UX audit exercises realistic synthetic scenarios as one user without developer directions. It must follow a journey across pages and then return to verify persisted state:

- **First run:** connect/import account, wait for sync, understand progress, recover from a failed import.
- **Daily 5-minute review:** spot new/unreviewed transactions, fix merchant/category, use filter/search, split or bulk edit, close review, return tomorrow without redoing work.
- **Monthly check:** explain income/spending, merchant/category trends, transfers, refund, credit-card payment, budget actual/rollover and unexplained differences.
- **Net worth check:** confirm cash/investments, historical value, missing/stale prices, FX and balance-observation reconciliations.
- **Recovery:** reconnect/retry after auth issue, duplicate import, partial failure, pending→posted, interrupted save; no silent loss or misleading success.
- **Mobile:** perform all in-scope tasks with touch-sized controls, no clipped table/modal/keyboard overflow, accessible focus and sensible navigation—not just page loads.

Use 2–3 desktop widths and at least one narrow mobile viewport for each changed critical journey. Capture sanitized screenshots/video or Playwright traces **for important before/after states**.

## Evidence-based scorecard per journey

For each relevant dimension assign `0=missing`, `1=broken`, `2=usable but rough`, `3=mature`, `4=excellent`, or `N/A` with an explanation. **No score without a screenshot, reproduction, test result or trace.** Treat a `2` as a UX-quality bug, not a pass.

| Dimension | What QA must observe |
| --- | --- |
| End-to-end coverage | The task can be discovered, started, completed and revisited without hidden steps; all in-scope substeps work |
| Behavioral correctness | Money, category, review, budget, transfer, valuation and status agree after every edit/retry |
| Friction and information architecture | Important action discoverable; minimal unnecessary navigation/clicks; no surprise resets or extra confirmations |
| Visual taste and consistency | Clear hierarchy, restrained density, aligned spacing, typography, readable figures, consistent icons, labels and controls |
| UI feedback and recovery | Loading, optimistic/pending, saved, error, empty, stale, partial, disconnected and retry states are intelligible and actionable |
| Cross-surface quality | Full task accessible on mobile, sensible layouts and touch targets; keyboard/focus/contrast and screen-reader labels as applicable |
| Resilience and performance | No race/duplication/crash; sensible interaction latency on target data scale; errors diagnosed and recoverable |
| Reference-behavior parity | Comparable outcome to supported Monarch reference, or explicitly documented and justified Offbook deviation/unsupported scope |

**Scoring rule:** every in-scope journey must have `>=3` in every applicable dimension. Any unexplained difference in money/account value, data loss, PII leak, blocked critical path or misleading status **fails regardless of numeric score**. An isolated `3` based on only a self-assertion is invalid; QA must independently interact with the app. Document each deviation and include a source-backed benchmark where available.

## Bug severity and polish bar

- **P0:** money/data loss or privacy/security breach; integration work stops except fixes.
- **P1:** wrong aggregate/ledger value, broken daily task or recovery, materially misleading sync/price status; priority fix before unrelated features.
- **P2—core UX blocker:** task technically works but has confusing workflow, silent/save uncertainty, broken mobile layout, inconsistent cross-feature semantics, poor discoverability, or a material benchmark gap. **Blocks Mature/DONE for any in-scope core journey**, even if CI is green.
- **P3:** isolated cosmetic imperfection with no meaningful task impact; may remain documented only if it does not break visual consistency at scale.

**Never reclassify UX failures as P3 just to close a milestone.** Product taste is evaluated through a UX critic checklist in a separate agent session; if disputed or unobservable, mark BLOCKED/UNVERIFIED rather than auto-PASS.

## Two complementary loops without per-PR AI QA

**Fast engineering loop:** every PR has required CI and behavior-specific tests. When touching UI, Engineering must perform a real browser walkthrough of changed user actions, inspect desktop/mobile and empty/loading/error states, and fix obvious interaction defects before merge. A component rendering is not enough. Ordinary PRs may auto-merge into **integration `main`** on CI green; merge ≠ mature.

**Independent UX critic loop (daily Product Review; hourly QA covers financial regression):** pin a single `main` SHA, replay high-risk journeys, independently inspect rendered UI and compare relevant official/observed Monarch behavior. Publish one quality scorecard with evidence, exact SHA, known gaps and linked Issues. Prioritize **P0/P1, then P2 core UX** before creating more features. Add repeatable browser tests/screenshots for defects found. The independent session is not the same one that wrote the code.

## Milestone graduation: only QA may propose MATURE

1. **Scope ledger:** every in-scope journey listed; no untested `N/A` or unjustified deferred user job; reference-confidence labels current.
2. **Deterministic correctness:** full CI/invariant suites, realistic synthetic fixtures, no P0/P1; P2 core UX issues resolved.
3. **Independent product-experience pass:** fresh end-to-end desktop/mobile session on the **exact candidate SHA**, scorecard evidence `>=3` all dimensions, scenarios including first-run/recovery and documented reference deviations.
4. **Stability soak:** repeated ingestion/retry and restart using realistic synthetic datasets, plus repeat QA after fixes; zero unresolved financial differences and no known release blockers. Document duration/environment/coverage instead of inventing a reliability percentage.
5. **Release proof:** backup restored successfully, migrations/rollback safe, live secrets never included in Cloud, final QA checkpoint, and a manually approved production promotion. A 30-day real-world dogfood period may then validate longer-term reliability; until completed, distinguish `release-ready` from `proven in sustained use`.

The **MATURE/DONE decision belongs to an independent final quality audit on the goal issue**. Engineering can mark individual implementation Issues closed but **must not close Goal #423 or write an unsupported parity claim**. If QA fails: open reproducible Issues, resume Engineering; re-audit the affected dimensions and integrated candidate, no invented confidence.

## Automated Goal #423 closure guard

A milestone cannot be marked complete merely by merging code. The [goal-maturity-guard.yml](../../.github/workflows/goal-maturity-guard.yml) GitHub Action listens for Goal #423 being closed as completed and checks the **latest** structured maturity certificate against current `main` SHA. If missing, stale, or incomplete, it automatically reopens the goal and explains why. For a public repository, the certificate is accepted only when the Issue comment is authored by the repository owner or a QA login explicitly authorized via `OFFBOOK_MATURITY_QA_ACTORS`; arbitrary public commenters cannot certify product maturity. `not_planned` cancellation is excluded.

The independent final QA session posts an ordinary Issue #423 comment containing one standalone line (not a code fence in the actual comment):

```text
<!-- offbook-maturity:v1 {"head_sha":"<40-char-current-main-sha>","result":"MATURE","qa_run_url":"https://github.com/.../actions/runs/...","zero_open_blockers":true,"soak_verified":true,"backup_restore_verified":true,"journeys":[{"id":"first-run","scores":{"coverage":3,"correctness":3,"friction":3,"visual":3,"feedback":3,"cross-platform":3,"resilience":3,"reference":3},"evidence":["https://github.com/.../issues/423#issuecomment-..."]}, ...]} -->
```

Exact required journey IDs: `first-run`, `daily-review`, `monthly-budget`, `net-worth`, `recovery`, `mobile`. Required dimensions: `coverage`, `correctness`, `friction`, `visual`, `feedback`, `cross-platform`, `resilience`, `reference`. Each must be an integer **3 or 4**, with one or more real HTTPS evidence links per journey; no skipped dimensions or placeholders. `zero_open_blockers` means QA actually inspected open P0/P1 and core-journey P2 Issues, not just ran CI. The backup-restore flag must be supported by an actual run log and may not be asserted by an isolated Cloud session that cannot see the operation.

**Limit:** this automated closure guard validates completeness, freshness and structured evidence; it cannot prove screenshots were inspected competently or detect unknown UX defects. QA must provide genuinely independent evaluation, rechecks and reasoned Monarch comparisons. The user does not need to review individual code PRs, but no system can objectively guarantee subjective taste or absolute commercial-product equivalence from agent scores alone.

## QA deliverable template

```md
### Product Experience Review — commit <40-char SHA>
Scope: <journey IDs> | Reference observation date: YYYY-MM-DD
Observed: <desktop/mobile device + viewport, synthetic fixture, run URL>
Journey | Coverage | Behavior | Friction | Visual | Feedback | Mobile | Reliability | Parity
... 0–4 / N/A, with a supporting link for each finding ...
Most painful friction (ranked): <actual clicks/misleading states>
Monarch differences: verified / unverified; official source or observed UI
P0/P1/P2 core/P3: <linked bugs>
Quality decision: MATURE / NOT MATURE / BLOCKED (with reasons)
```

**The test is whether a person would enjoy and trust using it repeatedly—not whether agents can make the test suite green.** A product that still feels like an unfinished developer tool must not pass.
