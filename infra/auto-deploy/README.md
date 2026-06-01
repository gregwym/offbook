# Pull-based auto-deploy (dev)

Auto-redeploys the `offbook-dev` stack on a self-hosted host (e.g. a Raspberry
Pi) whenever `origin/main` moves — **no GitHub Actions, no inbound webhook.**

A systemd timer polls `origin/main` every ~2 minutes. When the running build's
SHA (reported by `GET /health`, see [ADR-0016](../../docs/ADR/0016-tailscale-per-instance-deployment.md)
and `#310`) differs from `origin/main`, it fast-forwards `main` and runs
`make deploy` (using the host's `.env`). Because it only ever fetches and builds `main` (never PR/fork
code) and makes only outbound connections, it works behind NAT/Tailscale and
adds no attack surface — unlike a self-hosted GitHub Actions runner on a public
repo.

## One-time host setup

1. **Docker.** Install Docker Engine + the compose v2 plugin (64-bit OS):

   ```sh
   curl -fsSL https://get.docker.com | sh
   sudo usermod -aG docker "$USER"   # log out/in so the group takes effect
   ```

2. **Clone** the repo where the service expects it (default `~/offbook`):

   ```sh
   git clone https://github.com/gregwym/offbook.git ~/offbook
   cd ~/offbook
   ```

3. **Env.** Create `.env` (gitignored) — the single source of truth for this
   instance. Fill in `SESSION_SECRET`, `TS_HOSTNAME`, and any Plaid / Claude
   keys. The `OFFBOOK_PROJECT` + `OFFBOOK_COMPOSE_FILES` defaults from the example
   already select the dev stack + sidecar. **`TS_AUTHKEY` is not stored here** —
   it's passed once at first boot (next step):

   ```sh
   cp .env.example .env
   # edit .env: SESSION_SECRET=$(openssl rand -hex 32), TS_HOSTNAME=offbook-dev, etc.
   ```

4. **First boot.** Run `deploy` with the Tailscale auth key once — `deploy`
   sees the sidecar isn't up yet, so it brings up the full stack (postgres +
   backend + frontend + sidecar), stamped with the current SHA:

   ```sh
   make deploy TS_AUTHKEY=tskey-...      # reads .env (ENV_FILE defaults to .env)
   ```

   The node comes up at `offbook-dev.<tailnet>.ts.net` (first cert ~30s). See
   `infra/tailscale/README.md`. Every later `make deploy` detects the sidecar is
   already up and recreates only backend+frontend — no `TS_AUTHKEY` needed.

5. **Install the timer.** Edit `User=` and the two paths in the unit if you're
   not using `pi` / `~/offbook`, then:

   ```sh
   sudo cp infra/auto-deploy/offbook-deploy-dev.service /etc/systemd/system/
   sudo cp infra/auto-deploy/offbook-deploy-dev.timer   /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now offbook-deploy-dev.timer
   ```

## Operating it

```sh
# Watch deploys as they happen
journalctl -u offbook-deploy-dev -f

# Force a check now (instead of waiting for the next tick)
sudo systemctl start offbook-deploy-dev.service

# See when the timer next fires
systemctl list-timers offbook-deploy-dev.timer

# Pause / resume auto-deploy
sudo systemctl disable --now offbook-deploy-dev.timer
sudo systemctl enable  --now offbook-deploy-dev.timer
```

## Notes

- **Idempotent / self-healing.** No redeploy when already current. A failed
  build leaves the running version behind `origin/main`, so the next tick
  retries. If the backend is down, `/health` is unreachable → treated as
  "needs deploy" → the stack is rebuilt and recovers.
- **Single-flight.** A build can outlast the 2-minute interval; an `flock`
  guard skips overlapping runs rather than stacking them.
- **Manual deploys still work** — `make deploy` on the host coexists with the
  timer (both deploy the instance configured by `.env`).
- **Local edits on the host block deploys** on purpose: `git merge --ff-only`
  fails loudly if the checkout has diverged, rather than clobbering it. Keep
  the deploy checkout clean.
- **Private repo later?** Anonymous `git fetch` works because the repo is
  public. If you make it private, give the host a read-only deploy key or a
  token-backed remote so the fetch keeps working.
- **prod on the same host?** `bootstrap`/`deploy` are env-agnostic — create a
  `.env.prod` (see `.env.prod.example`) and run `make deploy ENV_FILE=.env.prod`.
  For a prod auto-deploy timer, copy the unit and set `OFFBOOK_ENV_FILE=.env.prod`
  in its `[Service]` `Environment=` (and probably gate prod behind tags rather
  than every `main` push).
