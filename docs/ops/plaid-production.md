# Plaid Production Access

Tracks issue #362 — the process-dependency half of "link a real bank, not just
sandbox." Sandbox stays the default everywhere in dev/QA/CI (see
`AGENTS.md` § Plaid Sandbox); this page is only about the production path.

## Status

| Acceptance criterion (#362) | Status |
|---|---|
| Plaid production (or limited-production) application submitted | **Owner action — not started.** Plaid's production application is a per-company/per-Plaid-account process (business details, use-case description, security questionnaire) that only the instance owner's Plaid account can submit. Tracked in comments on #362. |
| OAuth redirect URI strategy decided for Tailscale-private hosts | **Decided — see below.** |
| Production credentials live only in `.env.prod`; `PLAID_ENV=production` path smoke-tested | **Done.** `.env.prod.example` documents the keys (commented out, unset by default); `config.Load()` validates `PLAID_ENV ∈ {sandbox, development, production}` and requires `PLAID_TOKEN_KEY`/`PLAID_SECRET` alongside `PLAID_CLIENT_ID` regardless of env — see `backend/internal/config/config_test.go::TestLoad_PlaidProductionEnv`. |
| Docs: how a self-hoster brings their own Plaid production keys | **Done — see below.** |
| Sandbox remains the default everywhere in dev/QA/CI | **Already true** — `PLAID_ENV` defaults to `sandbox` when unset (`config.go`), and no dev/QA/CI env file sets it otherwise. |

The remaining item — actually filing the application — needs the owner's
Plaid account and business details. Everything an agent can prepare ahead of
that approval is done; this doc plus the config validation is the
agent-completable slice. See the comment on #362 for the day-one status.

## OAuth redirect URI strategy for Tailscale-private hosts

Plaid's OAuth-based institutions (most large US banks in **production** —
sandbox mostly doesn't require this) send the user back to a `redirect_uri`
that must be:

1. Registered in the Plaid dashboard ahead of time (allowlisted, exact match).
2. Reachable by the user's browser at the moment Plaid redirects back.

**Decision: use the instance's own Tailscale MagicDNS URL, not a third-party
redirect page.** Each Offbook instance already has a stable, HTTPS, per-instance
hostname from [ADR-0016](../ADR/0016-tailscale-per-instance-deployment.md) —
`https://<hostname>.<tailnet>.ts.net` — reachable by any device on the owner's
tailnet, including mobile. That satisfies both constraints without opening any
public ingress (which the "no webhooks" decision in the M14 sync ADR rules out
for the same reason).

Concretely: `redirect_uri` = `${FRONTEND_URL}/plaid/oauth-return` (a fixed SPA
route). This route does not exist yet — wiring `receivedRedirectUri` into
`usePlaidLink` and adding the `/plaid/oauth-return` route is Link-integration
work that belongs with the Link-token changes in **#364** (Plaid re-auth /
update-mode flow), not this tracking issue, since both touch the same
`CreateLinkToken` call path. This doc records the decision so #364 doesn't have
to re-derive it: reuse `FRONTEND_URL` (already required config, already
correct per-instance), don't introduce a second env var.

Constraints this implies, worth re-checking when #364 lands:

- `FRONTEND_URL` must be the tailnet MagicDNS URL, not `localhost` — already
  the case in `.env.prod.example`.
- The redirect URI must be registered **per institution** that requires OAuth
  in the Plaid dashboard once production access is granted — an owner action
  at that time, not a one-time setup step now.
- Self-hosters running their own instance register their **own** MagicDNS
  hostname as their own redirect URI under their own Plaid production
  application (see below) — there's nothing shared or hardcoded to Offbook's
  upstream repo.

## Bringing your own Plaid production keys (self-hosters)

Offbook never ships or proxies Plaid credentials — every self-hosted instance
uses its own Plaid account, same as Tailscale requires its own auth key
([ADR-0016](../ADR/0016-tailscale-per-instance-deployment.md)) and the same
`.env.prod`-is-secrets-only convention.

1. Create a Plaid account at https://dashboard.plaid.com if you don't have
   one, and apply for production access from the dashboard. This is a real
   review process with lead time — plan for weeks, not days.
2. Once approved, generate a production `client_id` and `secret` from the
   dashboard.
3. Generate a token-encryption key: `openssl rand -hex 32` (this is
   `PLAID_TOKEN_KEY`, unrelated to anything Plaid issues — see
   [ADR-0010](../ADR/0010-plaid-token-encryption.md)).
4. Set in `.env.prod` (never in `.env`, `.env.qa`, or any committed file):
   ```
   PLAID_CLIENT_ID=<your production client id>
   PLAID_SECRET=<your production secret>
   PLAID_ENV=production
   PLAID_TOKEN_KEY=<output of openssl rand -hex 32>
   ```
5. Register your instance's redirect URI (`https://<your-hostname>.<your-tailnet>.ts.net/plaid/oauth-return`)
   with Plaid for any OAuth institution you need, per the strategy above.
6. `command make deploy FLAVOR=prod` picks these up automatically — no code
   changes required. Dev, QA, and CI are unaffected; they never read
   `.env.prod`.

Sandbox development keys (`PLAID_ENV=sandbox`) remain free and require no
application — see `AGENTS.md` § Plaid Sandbox for the dev-loop setup.
