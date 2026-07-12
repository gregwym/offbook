# Pull-based auto-deploy

Auto-redeploys an Offbook instance on a self-hosted host (e.g. a Raspberry Pi)
whenever `origin/main` moves — **no GitHub Actions, no inbound webhook.**

Every `make deploy` (this timer or a manual run) ends with a post-deploy smoke
check and supports pinning/rolling back to a prior commit via
`GIT_REF=<sha>`. User-facing runbook — normal deploy, failed-deploy triage,
rollback, and the interaction with pre-migration backups — lives in
**[docs/ops/deploy-rollback.md](../../docs/ops/deploy-rollback.md)**. This file
covers the auto-deploy timer specifically.

A user-level systemd timer polls `origin/main` every ~2 minutes. When the
running build's SHA (reported by `GET /health`, see
[ADR-0016](../../docs/ADR/0016-tailscale-per-instance-deployment.md) and `#310`)
differs from `origin/main`, it fast-forwards `main` and runs `make deploy`.
Because it only ever fetches and builds `main` (never PR/fork code) and makes
only outbound connections, it works behind NAT/Tailscale and adds no attack
surface — unlike a self-hosted GitHub Actions runner on a public repo.

Everything is convention-driven: no editing of unit files, no sudo for install,
no secrets in the repo.

## One-time host setup

1. **Docker.** Install Docker Engine + the compose v2 plugin (64-bit OS):

   ```sh
   curl -fsSL https://get.docker.com | sh
   sudo usermod -aG docker "$USER"   # log out/in so the group takes effect
   ```

2. **Clone** the repo (any path — the installer records wherever you put it):

   ```sh
   git clone https://github.com/gregwym/offbook.git ~/offbook
   cd ~/offbook
   ```

3. **First boot.** Bring the stack up once with the Tailscale identity. `deploy`
   creates `.env`, generates `SESSION_SECRET`, and brings up the full stack
   (postgres + backend + frontend + sidecar), stamped with the current SHA:

   ```sh
   make deploy TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook
   ```

   The node comes up at `offbook.<tailnet>.ts.net` (first cert ~30s). Mint a
   per-instance, tagged key at <https://login.tailscale.com/admin/settings/keys>.
   `TS_AUTHKEY`/`TS_HOSTNAME` are read only at this first registration — neither
   is stored, and every later deploy needs neither. See `infra/tailscale/README.md`.

4. **Install the timer.** One command — no file editing, no sudo:

   ```sh
   make auto-deploy-install
   ```

   This renders a user-level systemd unit pointed at this checkout, enables
   `offbook-deploy@dev.timer`, and turns on lingering so it runs headless. For a
   prod instance, `make auto-deploy-install FLAVOR=prod`.

## Operating it

```sh
# Watch deploys as they happen
journalctl --user -u offbook-deploy@dev -f

# Force a check now (instead of waiting for the next tick)
systemctl --user start offbook-deploy@dev.service

# See when the timer next fires
systemctl --user list-timers offbook-deploy@dev.timer

# Pause / resume auto-deploy
systemctl --user disable --now offbook-deploy@dev.timer
systemctl --user enable  --now offbook-deploy@dev.timer

# Remove it entirely
make auto-deploy-uninstall
```

## Tearing down the instance

```sh
make down        # stop containers, keep data
make teardown    # stop AND drop volumes (postgres data + tailnet node identity)
```

`teardown` prompts for confirmation (pass `FORCE=1` to skip). After it removes
the `tailscale_state` volume, delete the stale node from the Tailscale admin
console if it lingers. Remember to `make auto-deploy-uninstall` too.

## Notes

- **Idempotent / self-healing.** No redeploy when already current. A failed
  build leaves the running version behind `origin/main`, so the next tick
  retries. If the backend is down, its reported SHA is empty → treated as
  "needs deploy" → the stack is rebuilt and recovers.
- **Port-independent health.** "What's deployed" comes from `make deployed-sha`,
  a container exec, so it works for prod too (prod publishes no host ports).
- **Single-flight.** A build can outlast the 2-minute interval; an `flock` guard
  (per flavor) skips overlapping runs rather than stacking them.
- **Manual deploys still work** — `make deploy` on the host coexists with the
  timer (both deploy the same instance).
- **Local edits on the host block deploys** on purpose: `git merge --ff-only`
  fails loudly if the checkout has diverged, rather than clobbering it. Keep the
  deploy checkout clean.
- **Private repo later?** Anonymous `git fetch` works because the repo is public.
  If you make it private, give the host a read-only deploy key or token-backed
  remote so the fetch keeps working.
- **prod on the same host?** `deploy` is flavor-aware — run the first boot with
  `FLAVOR=prod` and install a second timer with `make auto-deploy-install
  FLAVOR=prod`. The two timers share one templated unit but run independently.
  (Gate prod behind tags rather than every `main` push if you want it more
  conservative — adjust `OFFBOOK_DEPLOY_BRANCH` in the unit's environment.)
