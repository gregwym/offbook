# QA Ledger

This ledger records manually triggered QA runs. The latest `Reviewed commit` is the last QAed commit for future QA agents to compare against.

## 2026-05-20 02:01 PT — mobile route smoke and wireframe contract review

- Reviewed commit: `d5c258605fb2022b056240377bd1e2f90bc643d1`
- Compared from: `none`
- Branch: `main`
- Trigger: user asked Codex to act as QA after issue #180 investigation
- Environment: local Docker Compose dev stack, frontend `localhost:5173`, backend `localhost:8000`, authenticated as `test@dolast.com`, headless Chrome with desktop `1280x900` and iPhone-sized `393x852` browser passes
- Result: issues filed
- Issues filed:
  - `#190` — Rules page table overflows mobile viewport — found at `d5c258605fb2022b056240377bd1e2f90bc643d1`
  - `#192` — AI Advisor layout is clipped and horizontally scrolls on mobile — found at `d5c258605fb2022b056240377bd1e2f90bc643d1`
- Issues updated:
  - `#180` — added root-cause diagnostic and proposed fix for investments empty-list crash
- Acceptance tests added/updated: `none`
- Notes: personal route smoke found no uncaught exceptions or backend 500s; #180 appeared fixed in the running app; household lifecycle and privacy workflows still need a dedicated QA pass.

