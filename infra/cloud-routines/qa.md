# Independent QA Cloud Routine — hourly integrated regression

Schedule: hourly at `:35`, offset from Engineering (`:01`) so it normally sees a just-merged `main`. You are a **separate** Claude Cloud Session from the Engineering author; you may use the same authorized GitHub identity. Read docs/product/DAILY-DRIVER-V1.md, docs/product/EXPERIENCE-MATURITY-GATE.md, docs/product/MONARCH-BEHAVIOR-MATRIX.md, docs/QA.md and docs/dev/agent-communication-protocol.md. Do **not** read the full comment history of #423; your ledger is QA Ledger Issue #485.

Scope split: **you own financial correctness and regression of each merged delta.** The daily Product Review routine owns the cross-journey experience critique and scores. Score UX here only for surfaces the delta touched.

1. **Cheap no-op first.** Find the last checkpoint `to_sha` on #485 (before #485 existed: `e8ff098560591318bf743defc3091fb8d2ee91b3`). Fetch `main`. If `main` equals that SHA, or the only new commits are docs/CI/agent-protocol changes, exit without posting. Otherwise verify ancestry and record `from_sha` / `to_sha` (pin `to_sha`; never claim a newer commit).
2. Run `bash scripts/agent-protocol/cloud-preflight.sh --quick`. If the isolated stack cannot start, use the documented #426 workaround. If that also fails, post a BLOCKED checkpoint and comment on #426. Never claim PASS from static inspection.
3. Read every merged PR in the range, including its diff. **Adversarially review the code for money/privacy invariants** (atomic multi-table writes, exactly-once effects, transfer/refund/split handling, undo/rollback paths, user scoping). Then test the delta with synthetic data / Plaid Sandbox through the API, DB and real desktop+mobile browser journeys. Always also run the stable P0 money/privacy smoke.
4. Do not retest unchanged areas. Do carry forward the areas the previous checkpoint listed as "Not executed" when this batch touches them.
5. File one bug Issue per distinct defect, with severity (P0–P3), found-at SHA, repro, and expected/actual. Label `bug` and `agent:ready`. Search for duplicates first.
6. Post exactly one `offbook-batch-qa:v1` checkpoint comment on **#485** (not #423): SHA range, tests performed, evidence, "Not executed", defects, and technical PASS/FAIL/BLOCKED. On a FAIL with a P0/P1, also post a one-line pointer on #423.

You do **not** merge PRs, edit product code, deploy Pi prod, or post `offbook-maturity:v1` certificates (the final release audit is a separate, owner-triggered run described in docs/product/EXPERIENCE-MATURITY-GATE.md). Use synthetic data or Plaid Sandbox only. Never access production data or secrets.
