# Pull-based auto-deploy (dev)

Auto-redeploys the `offbook-dev` stack on a self-hosted host (e.g. a Raspberry
Pi) whenever `origin/main` moves — **no GitHub Actions, no inbound webhook.**

A systemd timer polls `origin/main` every ~2 minutes. When the running build's
SHA (reported by `GET /health`, see [ADR-0016](../../docs/ADR/0016-tailscale-per-instance-deployment.md)
and `#310`) differs from `origin/main`, it fast-forwards `main` and runs
`make deploy-dev`. Because it only ever fetches and builds `main` (never PR/fork
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

3. **Env.** Create `.env` (gitignored) with at least a real `SESSION_SECRET`,
   plus any Plaid / Claude keys you want this instance to have:

   ```sh
   cp .env.example .env
   # edit .env: SESSION_SECRET=$(openssl rand -hex 32), etc.
   ```

4. **Bring the full stack up once** with `bootstrap-dev` — this creates
   postgres + backend + frontend + the Tailscale sidecar. (`deploy-dev` can't
   do first boot: it runs `--no-deps` without the sidecar override, so postgres
   and Tailscale must already exist.) `bootstrap-dev` stamps the build with the
   current SHA, exactly like `deploy-dev`, so `/health` reports a real commit
   from the start instead of `dev`:

   ```sh
   TS_AUTHKEY=tskey-auth-... make bootstrap-dev      # TS_HOSTNAME defaults to offbook-dev
   ```

   The node comes up at `offbook-dev.<tailnet>.ts.net` (first cert ~30s). See
   `infra/tailscale/README.md`. `TS_AUTHKEY` is only needed for this one-time
   boot; the timer (and `deploy-dev`) never touch the sidecar again.

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
- **Manual deploys still work** — `make deploy-dev` (local) or
  `make deploy DEPLOY_HOST=...` (from a laptop) coexist with the timer.
- **Local edits on the host block deploys** on purpose: `git merge --ff-only`
  fails loudly if the checkout has diverged, rather than clobbering it. Keep
  the deploy checkout clean.
- **Private repo later?** Anonymous `git fetch` works because the repo is
  public. If you make it private, give the host a read-only deploy key or a
  token-backed remote so the fetch keeps working.
- **prod** is not wired here yet — there's no `make deploy-prod` target. When
  it lands, copy this directory to an `offbook-prod`-flavored timer.
