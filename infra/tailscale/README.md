# Tailscale Sidecar

Pattern for exposing an Offbook instance over HTTPS at a `*.ts.net` MagicDNS hostname. See [ADR-0016](../../docs/ADR/0016-tailscale-per-instance-deployment.md) for the decision.

## One-time setup

1. Have a Tailscale account and tailnet.
2. In the admin console, create a tag for Offbook nodes: `tag:offbook` (assign your user as the owner so you can mint keys for it).
3. Mint a per-instance auth key, **tagged** `tag:offbook`. Reusable + ephemeral is fine for a personal tailnet; choose per your threat model.

## Bring up an instance

```sh
TS_AUTHKEY=tskey-auth-... TS_HOSTNAME=offbook-dev \
  docker compose -p offbook-dev \
    -f docker-compose.yml \
    -f docker-compose.tailscale.yml \
    up -d
```

The node appears in the tailnet as `offbook-dev.<tailnet>.ts.net` once Tailscale has fetched a cert (first boot takes ~30s).

## Bring up a second instance on the same host

Same command, different project name + hostname + auth key. Compose project name namespaces containers, networks, and named volumes (including `postgres_data` and `tailscale_state`), so there's no collision:

```sh
TS_AUTHKEY=tskey-auth-... TS_HOSTNAME=offbook-prod \
  docker compose -p offbook-prod \
    -f docker-compose.yml \
    -f docker-compose.tailscale.yml \
    up -d
```

(A dedicated `docker-compose.prod.yml` with a different DB name and prod env defaults is a planned follow-up; until it lands, this exposes the dev-flavored stack under the prod hostname.)

## Notes

- The sidecar runs `tailscale serve` in userspace mode — no `CAP_NET_ADMIN`, no `/dev/net/tun` mount, no host Tailscale install.
- `serve.json` uses `${TS_CERT_DOMAIN}` — Tailscale substitutes the node's full MagicDNS name at read time. The same file works for every instance.
- Funnel (public-internet exposure) is explicitly disabled in `serve.json`. Enabling it is a separate decision; see the follow-up note in ADR-0016.
- The base `docker-compose.yml` still binds host ports for local dev. They're redundant when accessing via Tailscale but harmless. Override with `ports: !override []` in a site-specific compose file if local exposure is a concern.
