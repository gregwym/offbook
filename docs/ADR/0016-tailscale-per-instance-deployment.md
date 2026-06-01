# ADR 0016: Local Multi-Instance Deployment via Per-Instance Tailscale Sidecars

## Status
Accepted

## Context
Offbook is intentionally self-hostable and runs as a Docker Compose stack rather than as a packaged binary or desktop app (see [ADR-0002](0002-postgres-over-sqlite.md) — Postgres dependency rules out a single-file distribution without a SQLite rewrite). Operators commonly want to run more than one instance on the same host: a stable instance against their real data, and a development/preview instance for testing changes before they touch the real DB.

We need a deployment pattern that:
1. Runs multiple instances side-by-side on one host without port/volume collisions.
2. Exposes each instance over HTTPS at a stable hostname, ideally without owning a public domain or punching holes in NAT.
3. Keeps the per-instance footprint thin — no per-instance reverse proxy, no per-instance certificate management, no per-instance DNS surgery.
4. Doesn't require packaging the app (deferred — Postgres + the React frontend make a single binary a real project, not a side quest).

Tailscale Serve is a natural fit for (2): every node on a tailnet gets a `<hostname>.<tailnet>.ts.net` MagicDNS name and a managed Let's Encrypt cert, with no inbound NAT exposure. The wrinkle: a single Tailscale node owns exactly one ts.net hostname, and `tailscale serve` doesn't do Host-header virtual-host routing. So "multiple instances on one host" must be solved at the Tailscale layer, not by stuffing multiple apps behind a shared node.

## Decision
Each Offbook instance runs as a separate Docker Compose **project** with its own Tailscale **sidecar container**. One node per instance, one MagicDNS hostname per instance.

Concretely:
1. **`docker-compose.tailscale.yml`** is a reusable, instance-agnostic override that adds a `tailscale` service. Required env: `TS_AUTHKEY` (auth key), `TS_HOSTNAME` (the desired MagicDNS name).
2. **Instance identity is the compose project name** (`docker compose -p offbook-dev ...` vs `-p offbook-prod ...`). Project name namespaces containers, networks, and named volumes — so two instances on one host don't fight over `postgres_data`.
3. **The sidecar joins the tailnet, runs `tailscale serve` against the in-compose frontend service**, and terminates HTTPS at `:443` on the tailnet-facing interface. The frontend container does not bind a host port for tailnet-exposed instances; only the sidecar is reachable from the tailnet.
4. **Per-instance auth keys** — each sidecar gets its own ephemeral or reusable auth key, ideally tagged (`--advertise-tags=tag:offbook`) so ACLs and key rotation can target Offbook nodes without touching the rest of the tailnet.

## Rationale
- **One node per instance is the only Tailscale-native way to get one ts.net hostname per instance.** `tailscale serve` is path/port-aware, not Host-header-aware. Multiplexing two apps behind one node forces either path prefixes (`/dev`, `/prod`) — which the React app would have to know about via `vite.config.ts` `base`, cookie path scoping, and absolute API URLs — or alternate ports (`:443`, `:8443`), which works but is uglier and breaks browser-managed assumptions like cookie defaults.
- **Sidecar containers don't need root or `CAP_NET_ADMIN` on the host** — userspace networking (`TS_USERSPACE=true`) is sufficient for Serve, which keeps the operator story "docker compose up" rather than "install Tailscale on the host first." It also means the host's own tailnet identity (if any) is untouched.
- **Compose project name as the isolation boundary** is already how `docker-compose.qa.yml` works (`-p offbook-qa`). Reusing the pattern keeps mental overhead low: a new instance is `-p <name>` + a base/override pair, not a new abstraction.
- **Instance-agnostic override** means the same `docker-compose.tailscale.yml` works with the current dev stack, a future `docker-compose.prod.yml`, and even the QA stack. The override knows nothing about which environment it's exposing; it just proxies whichever `frontend` service exists in the merged composition.
- **Per-instance auth keys with tags** keep blast radius bounded. A leaked dev key doesn't grant tailnet access beyond the dev node and can be revoked without touching prod. Tagged nodes also let ACLs say "tag:offbook nodes are reachable by tag:family" without enumerating hostnames.

## Consequences
- **`docker-compose.tailscale.yml`** lands in the repo root, composable with any base stack:
  ```sh
  TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook-dev \
    docker compose -p offbook-dev -f docker-compose.yml -f docker-compose.tailscale.yml up -d
  ```
- **Operators need a Tailscale account and tailnet.** This is the cost of the simplicity — no DDNS, no reverse proxy, no certbot, but the operator is now Tailscale-dependent. Acceptable: Tailscale's free tier covers personal/family use, and the alternative (Caddy + DDNS + LetsEncrypt) is materially more setup.
- **Per-instance Tailscale state is volume-backed** so a sidecar restart doesn't re-key the node and rotate the cert. State volume is namespaced by compose project name, so dev and prod state can't collide.
- **The base `docker-compose.yml` does not change.** It still binds host ports for local development (`5173`, `8000`, `5432`). When exposed via Tailscale, those host ports are redundant but harmless; operators concerned about local exposure can override with `ports: !override []` in their site-specific compose file. We don't bake that in because it would break the documented local-dev experience.
- **A real `docker-compose.prod.yml`** (different DB name, different volume, prod env defaults, no init scripts that create dev/test DBs) is not part of this ADR — it's a separate follow-up. This ADR makes the prod stack *possible*; it doesn't write it. Filed as backlog.
- **AGENTS.md gains a short "Tailscale deployment" section** pointing at this ADR and the override.

## Alternatives Considered
- **Single Tailscale node, path-based multiplexing (`/dev`, `/prod`).** Rejected. Forces React app to know its base path, breaks cookie defaults, makes absolute API URLs in the frontend a footgun. The cleanup work in the app would dwarf the infrastructure savings.
- **Single Tailscale node, port-based multiplexing (`:443`, `:8443`).** Workable but uglier — users have to remember non-standard ports, and browser features (HSTS preloading, secure-cookie defaults) get awkward across mixed ports.
- **Tailscale on the host, no sidecars.** Couples app deployment to host config; operator has to install Tailscale on the host *and* manage `tailscale serve` outside Compose. Loses the "one `docker compose up` and you're done" property.
- **Caddy reverse proxy + DDNS + LetsEncrypt.** Strictly more general but materially more setup per operator. Tailscale gives us MagicDNS, cert provisioning, and zero-trust ingress in one dependency.
- **Package as a single binary / `.app`.** Discussed and deferred — Postgres dependency makes this a real project (SQLite migration or bundled Postgres process). Not blocked by this ADR; orthogonal.

## Follow-up
- **Backlog: ACL template** under `infra/tailscale/` showing the tag scheme (`tag:offbook`, `tag:offbook-prod`, `tag:offbook-dev`) and a sample ACL granting the operator's tag access to both.
- **Backlog: Funnel exposure** (`tailscale funnel`) for instances the operator wants reachable from the public internet. Not in scope for this ADR — Funnel changes the threat model significantly and deserves its own decision.

## Addendum (#328): near-zero-config deploy

The first cut of `make deploy` was "env-agnostic": a single generic target whose
entire shape came from the env file, which therefore had to declare
`OFFBOOK_PROJECT` and `OFFBOOK_COMPOSE_FILES` (the compose project name and
overlay list) alongside its secrets. That conflated three different kinds of
input and made every operator hand-write compose plumbing for the common
one-instance case. We tightened it to **convention over configuration**:

1. **Compose plumbing is convention, selected by `FLAVOR`.** `make deploy`
   (FLAVOR=dev) → project `offbook`, overlays `base + tailscale`, env `.env`;
   `make deploy FLAVOR=prod` → `offbook-prod`, `base + prod + tailscale`,
   `.env.prod`. `OFFBOOK_PROJECT`/`OFFBOOK_COMPOSE_FILES` remain honored as an
   env-file **escape hatch** for exotic multi-instance hosts, but are no longer
   required and are gone from the standard examples.
2. **The env file holds secrets only, and `make deploy` bootstraps it.** On
   first boot it creates the file from `<env>.example` and generates a
   `SESSION_SECRET` if absent — never rotating an existing one (rotation
   invalidates sessions and stored Claude keys; see `.env.example`).
3. **Tailscale identity is bootstrap-only, like the auth key already was.**
   `TS_HOSTNAME` joins `TS_AUTHKEY` as a command-line-only, first-boot value —
   both are read only when the sidecar registers. After first boot, deploys
   recreate only `backend`+`frontend` (`--no-deps`), so the sidecar (and its
   `TS_HOSTNAME`) is never touched; identity lives in the `tailscale_state`
   volume. Neither value is ever stored in the repo or env file.
4. **Auto-deploy and teardown are no-edit make targets.** Auto-deploy moved from
   a system-level unit requiring hand-edited `User=`/paths to a **user-level**,
   FLAVOR-templated systemd unit (`offbook-deploy@<flavor>`) installed by
   `make auto-deploy-install` (renders the unit from a checked-in template,
   enables the timer, turns on lingering — no sudo, no editing).
   `make auto-deploy-uninstall`, `make down` (stop), and `make teardown`
   (drop volumes, de-register the node) round out a symmetric lifecycle. The
   timer reads "what's deployed" via `make deployed-sha` (a container exec), so
   it works for prod too, which publishes no host ports.

Net operator surface for the common case: `make deploy TS_AUTHKEY=... TS_HOSTNAME=...`
once, `make deploy` thereafter, `make auto-deploy-install` to automate — no file
editing at any step.

Also in #328: a real `docker-compose.prod.yml` landed (separate DB name,
named volume, no host ports, fail-fast on missing `SESSION_SECRET`), closing the
earlier "Backlog: `docker-compose.prod.yml`" follow-up.
