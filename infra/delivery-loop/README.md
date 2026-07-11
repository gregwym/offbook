# Autonomous Delivery Loop — local launchd driver

Runs the production-readiness delivery loop
([docs/dev/autonomous-delivery.md](../../docs/dev/autonomous-delivery.md))
**headless on the owner's Mac**: a user-level LaunchAgent fires
`run-iteration.sh` on an interval; each firing runs one `claude -p` iteration
(Sonnet, permissions bypassed) that ships **at most one PR** and exits.

## Why local, not a cloud routine

The claude.ai cloud-routine experiment (July 2026) *did* deliver — five issues
merged overnight — but each firing pushed interactive permission approvals to
the owner's phone (not autonomous) and its sessions were not observable in the
Claude iOS app. Running locally fixes both: `--dangerously-skip-permissions`
means zero approval prompts, and every iteration writes a plain log file you
can `tail`. The env is also already warm — local git + `gh` credentials,
Colima/Docker Postgres for `make verify` — no cold-start cost.

## Install / manage

```sh
command make delivery-install      # from the repo root
command make delivery-uninstall
```

Install creates a **dedicated clone** at `~/src/offbook-delivery` (the loop
never touches your working checkout — no branch/index fights with an active
dev session), renders `com.offbook.delivery.plist`, and bootstraps it into
launchd. Idempotent; re-run to refresh.

| Knob (env var at install time) | Default | Meaning |
|---|---|---|
| `OFFBOOK_DELIVERY_INTERVAL` | `10800` (3 h) | Seconds between firings |
| `OFFBOOK_DELIVERY_DIR` | `~/src/offbook-delivery` | Delivery clone location |
| `OFFBOOK_DELIVERY_MODEL` | `claude-sonnet-5` | Model for the iteration (runner env) |
| `OFFBOOK_DELIVERY_MAX_SECONDS` | `10800` | Watchdog kills a runaway iteration (runner env) |

Ad-hoc control:

```sh
launchctl kickstart gui/$(id -u)/com.offbook.delivery   # fire one iteration now
launchctl bootout   gui/$(id -u)/com.offbook.delivery   # pause (uninstall re-arms)
tail -f ~/Library/Logs/offbook-delivery/iteration-*.log # watch a run
```

## Behavior & safety

- **One iteration = one PR max.** The prompt (`iteration-prompt.md`) enforces
  idempotent pick (existing PR → finish it, never duplicate), CI gating before
  merge, and stop-on-blocker. `main` stays PR-only protected.
- **Quota is the governor.** A firing that hits the plan's usage cap fails
  cheaply; the next firing (after the window resets) resumes from GitHub
  state. Nothing to babysit. To leave more daytime quota for interactive use,
  reinstall with a longer interval (e.g. `OFFBOOK_DELIVERY_INTERVAL=21600`).
- **Overlap-safe.** launchd never double-starts the label, a stale-aware lock
  guards manual runs, and a wall-clock watchdog kills a hung iteration.
- **Permissions are bypassed** (`--dangerously-skip-permissions`) — that is
  the point, and it is why this runs only on the owner's own machine, scoped
  by the prompt to this repo + its toolchain. Do not reuse this harness on a
  shared host.
- Logs older than 14 days are pruned automatically.

## Files

| File | Role |
|---|---|
| `iteration-prompt.md` | The self-contained prompt each firing executes |
| `run-iteration.sh` | Lock → refresh clone → `claude -p` → watchdog → log |
| `com.offbook.delivery.plist.template` | LaunchAgent, rendered by install.sh |
| `install.sh` / `uninstall.sh` | Manage the LaunchAgent + delivery clone |
