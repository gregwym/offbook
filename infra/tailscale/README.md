# Tailscale Sidecar

Pattern for exposing an Offbook instance over HTTPS at a `*.ts.net` MagicDNS hostname. See [ADR-0016](../../docs/ADR/0016-tailscale-per-instance-deployment.md) for the decision.

## One-time setup

1. Have a Tailscale account and tailnet.
2. Decide how the sidecar nodes will be tagged. Two common shapes:
   - **Reusable key with auto-applied tag (recommended):** define an Offbook tag in your ACL policy (e.g. `tag:offbook`) and mint a reusable + ephemeral key that auto-applies it. The sidecar inherits the tag automatically; leave `TS_EXTRA_ARGS` unset.
   - **Untagged key + sidecar-side advertise:** mint an untagged key and set `TS_EXTRA_ARGS=--advertise-tags=tag:offbook` in your env file. Requires the key's identity to have permission for the tag, or registration fails.
3. ACL recommendation: restrict whatever tag you use so Offbook nodes can only be reached by you, not the broader tailnet.

## Bring up an instance

```sh
TS_AUTHKEY=tskey-auth-... TS_HOSTNAME=offbook-dev \
  docker compose -p offbook-dev \
    -f docker-compose.yml \
    -f docker-compose.tailscale.yml \
    up -d
```

The node appears in the tailnet as `offbook-dev.<tailnet>.ts.net` once Tailscale has fetched a cert (first boot takes ~30s).

## Bring up a second (prod-flavored) instance on the same host

Same command, but compose in `docker-compose.prod.yml` and pass a prod env file. Compose project name namespaces containers, networks, and named volumes (including `prod_postgres_data` and `tailscale_state`), so there's no collision with the dev stack:

```sh
cp .env.prod.example .env.prod
# Fill in SESSION_SECRET (openssl rand -hex 32), TS_AUTHKEY, FRONTEND_URL

docker compose -p offbook-prod \
  --env-file .env.prod \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  -f docker-compose.tailscale.yml \
  up -d
```

The prod override swaps `POSTGRES_DB` to `offbook_prod`, uses a named volume (no collision with the dev stack's `./data/postgres` bind mount), unbinds all host ports (only the Tailscale sidecar is reachable), and refuses to start without `SESSION_SECRET`.

## Notes

- The sidecar runs `tailscale serve` in userspace mode — no `CAP_NET_ADMIN`, no `/dev/net/tun` mount, no host Tailscale install.
- `serve.json` uses `${TS_CERT_DOMAIN}` — Tailscale substitutes the node's full MagicDNS name at read time. The same file works for every instance.
- Funnel (public-internet exposure) is explicitly disabled in `serve.json`. Enabling it is a separate decision; see the follow-up note in ADR-0016.
- The base `docker-compose.yml` still binds host ports for local dev. They're redundant when accessing via Tailscale but harmless. Override with `ports: !override []` in a site-specific compose file if local exposure is a concern.
