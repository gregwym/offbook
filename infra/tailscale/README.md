# Tailscale Sidecar

Pattern for exposing an Offbook instance over HTTPS at a `*.ts.net` MagicDNS hostname. See [ADR-0016](../../docs/ADR/0016-tailscale-per-instance-deployment.md) for the decision.

## One-time setup

1. Have a Tailscale account and tailnet.
2. Decide how the sidecar nodes will be tagged. Two common shapes:
   - **Reusable key with auto-applied tag (recommended):** define an Offbook tag in your ACL policy (e.g. `tag:offbook`) and mint a reusable + ephemeral key that auto-applies it. The sidecar inherits the tag automatically; leave `TS_EXTRA_ARGS` unset.
   - **Untagged key + sidecar-side advertise:** mint an untagged key and set `TS_EXTRA_ARGS=--advertise-tags=tag:offbook` in your env file. Requires the key's identity to have permission for the tag, or registration fails.
3. ACL recommendation: restrict whatever tag you use so Offbook nodes can only be reached by you, not the broader tailnet.

## Bring up an instance

The supported path is `make deploy`, which picks the project name + overlays by
convention and passes the Tailscale identity through to the sidecar on first
boot:

```sh
make deploy TS_AUTHKEY=tskey-auth-... TS_HOSTNAME=offbook   # first boot
make deploy                                                 # later updates
```

The node appears in the tailnet as `offbook.<tailnet>.ts.net` once Tailscale has fetched a cert (first boot takes ~30s). Under the hood this is just:

```sh
TS_AUTHKEY=tskey-auth-... TS_HOSTNAME=offbook \
  docker compose -p offbook \
    -f docker-compose.yml \
    -f docker-compose.tailscale.yml \
    up -d
```

## Bring up a second (prod-flavored) instance on the same host

Add `FLAVOR=prod`. It composes in `docker-compose.prod.yml`, uses the `offbook-prod` project name, and reads `.env.prod`. Compose project name namespaces containers, networks, and named volumes (including `prod_postgres_data` and `tailscale_state`), so there's no collision with the dev stack:

```sh
make deploy FLAVOR=prod TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook-prod   # first boot
make deploy FLAVOR=prod                                                  # updates
```

`deploy` creates `.env.prod` and fills `SESSION_SECRET` on first boot; set `FRONTEND_URL` in it by hand. The prod override swaps `POSTGRES_DB` to `offbook_prod`, uses a named volume (no collision with the dev stack's `./data/postgres` bind mount), unbinds all host ports (only the Tailscale sidecar is reachable), and refuses to start without `SESSION_SECRET`.

## Notes

- The sidecar runs `tailscale serve` in userspace mode — no `CAP_NET_ADMIN`, no `/dev/net/tun` mount, no host Tailscale install.
- `serve.json` uses `${TS_CERT_DOMAIN}` — Tailscale substitutes the node's full MagicDNS name at read time. The same file works for every instance.
- Funnel (public-internet exposure) is explicitly disabled in `serve.json`. Enabling it is a separate decision; see the follow-up note in ADR-0016.
- The base `docker-compose.yml` still binds host ports for local dev. They're redundant when accessing via Tailscale but harmless. Override with `ports: !override []` in a site-specific compose file if local exposure is a concern.
