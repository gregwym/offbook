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
route). Implemented in #502:

- **Backend rule** (`config.Config.PlaidRedirectURI`): an explicit
  `PLAID_REDIRECT_URI` always wins. Otherwise `PLAID_ENV=production` derives
  `${FRONTEND_URL}/plaid/oauth-return` (trailing slash trimmed); `sandbox` and
  `development` leave it **unset**, because Plaid rejects a `redirect_uri`
  that isn't registered in the dashboard and dev/QA/CI never register one.
  The URI goes on every link token — new links *and* update-mode re-auth
  (#364), since an OAuth bank needs the redirect for both.
- **Frontend**: before opening Link, Add account and the Settings
  "Reconnect" button stash the link token (plus mode, expiration and, for
  re-auth, the item id) in `sessionStorage` (`src/lib/plaidOAuth.ts`). The
  bank's sign-in redirects the whole tab back to `/plaid/oauth-return`, which
  re-initializes Link with that token and
  `receivedRedirectUri = window.location.href`, then finishes the same chain
  as the originating page: exchange → sync-accounts → sync-transactions (new
  link, lands on Accounts) or just the syncs (re-auth, lands on Settings).
  It is a full-page redirect, not a popup, so it works on mobile Safari and
  Chrome. The stored token is dropped as soon as Link succeeds, and the
  linked item id is kept until the import finishes, so reloading the return
  page resumes the syncs instead of reopening Link or exchanging twice.
- **Failure states** on the return page — no stored session (different tab,
  storage cleared), expired token (locally, or Plaid's `INVALID_LINK_TOKEN`),
  user cancelled, exchange/sync failed — each show a message and a way back
  (Add account or Settings; "Retry import" when the bank already linked).

Constraints this implies:

- `FRONTEND_URL` must be the tailnet MagicDNS URL (`https://…ts.net`), not
  `localhost` — already the case in `.env.prod.example`. Plaid requires
  HTTPS for production redirect URIs.
- The sign-in session cookie is `SameSite=Lax`, so it survives the
  top-level redirect back from the bank.
- Self-hosters running their own instance register their **own** MagicDNS
  hostname as their own redirect URI under their own Plaid production
  application (see below) — there's nothing shared or hardcoded to Offbook's
  upstream repo.

### Owner steps to enable OAuth banks

1. In the Plaid dashboard → **Developers → API → Allowed redirect URIs**, add
   exactly `https://<hostname>.<tailnet>.ts.net/plaid/oauth-return` (the
   value of `FRONTEND_URL` in `.env.prod` plus `/plaid/oauth-return`; no
   trailing slash, exact match).
2. Make sure `.env.prod` has `PLAID_CLIENT_ID`, `PLAID_SECRET`,
   `PLAID_ENV=production`, `PLAID_TOKEN_KEY` and `FRONTEND_URL` set as below.
   `PLAID_REDIRECT_URI` is not needed unless the URI differs from the
   derived one.
3. Complete any per-institution OAuth registration the Plaid dashboard
   lists for your account (some banks, e.g. Chase, require it before they
   appear in production Link).
4. Redeploy (`command make deploy FLAVOR=prod`), then Add account → Connect
   a bank → pick the OAuth bank. After the bank sign-in you should land on
   `/plaid/oauth-return` briefly, then on Accounts with the new accounts.

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
   in the Plaid dashboard, per "Owner steps to enable OAuth banks" above.
6. `command make deploy FLAVOR=prod` picks these up automatically — no code
   changes required. (`docker-compose.prod.yml` binds each `PLAID_*` key
   explicitly, because the prod overlay drops the base `env_file`; before
   #502 the keys never reached the backend and Plaid stayed disabled in prod.) Dev, QA, and CI are unaffected; they never read
   `.env.prod`.

Sandbox development keys (`PLAID_ENV=sandbox`) remain free and require no
application — see `AGENTS.md` § Plaid Sandbox for the dev-loop setup.
