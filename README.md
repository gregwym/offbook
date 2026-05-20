# Offbook

Privacy-first personal finance app. Your data stays on your machine.

## Why

Existing finance tools (Mint, YNAB, Copilot) require sharing your financial data with third-party servers. Offbook runs entirely on localhost — self-hostable via Docker if you want remote access.

## Features (planned)

- **Account aggregation** via Plaid (sandbox → production)
- **Transaction categorization** with customizable rules
- **Budget tracking** with alerts
- **Savings goals** with progress tracking
- **Investment portfolio** tracking (stocks, ETFs, crypto with full precision)
- **AI financial assistant** (Claude API or local Ollama) — uses only anonymized, aggregated data

## Privacy architecture

- PII (account numbers, holder names) stored in an isolated `pii_store` table
- AI assistant has **no access** to PII — enforced architecturally, not by convention
- All data stays in your local PostgreSQL instance
- See [docs/ADR/0003-pii-isolation-table.md](docs/ADR/0003-pii-isolation-table.md) for details

## Quick start

```bash
cp .env.example .env
# Edit .env with your Plaid keys (optional) and AI keys (optional)
docker compose up
```

- Frontend: http://localhost:5173
- Backend API: http://localhost:8000/api/v1/health

## Tech stack

Go + Gin | PostgreSQL | React + Vite + TypeScript | Docker Compose

## Running the app

Three supported ways to run, pick one:

### 1. Full Docker (production-ish)

Builds backend + frontend images, serves the static Vite bundle via nginx. No live reload.

```bash
docker compose up
```

Use this for: trying the app, demos, or self-hosting.

### 2. Dev Docker (live reload)

Backend runs under [air](https://github.com/cosmtrek/air), frontend runs Vite dev server, both inside containers. Source-mounted, so edits reload automatically.

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up
```

**Prerequisite (Colima/Podman/Docker Desktop):** the VM needs ≥4 GiB or the backend's first cold compile will be OOM-killed. For Colima:

```bash
colima stop
colima start --memory 4 --save-config
```

See [docs/dev/colima.md](docs/dev/colima.md) for details. Docker Desktop / Podman Desktop users: bump the VM memory in the GUI.

Use this for: most active development.

### 3. Native dev (fastest iteration)

Run backend and frontend directly on the host; only Postgres in Docker.

```bash
docker compose up -d postgres                  # Postgres only
(cd backend && make dev)                       # Go server on :8000
(cd frontend && pnpm install && pnpm dev)      # Vite on :5173
```

Backend helpers: `make smoke` (start + wait for /health), `make stop` (kill by port), `make test`, `make verify`. See [AGENTS.md](AGENTS.md) for the full list.

Use this for: tight Go iteration loops where container rebuild latency hurts.

### Accessing from another device on your LAN (phone, tablet)

Options 1 and 2 already bind to all interfaces, so just point the device at your machine's LAN IP:

```bash
ipconfig getifaddr en0    # macOS: prints e.g. 192.168.x.x
```

Then open `http://<that-ip>:5173`. The first connection may trigger a macOS firewall allow prompt.

Option 3 needs Vite told explicitly: `pnpm dev --host 0.0.0.0`.

## Development

See [AGENTS.md](AGENTS.md) for session conventions and commands, [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full technical reference, and [docs/ROADMAP.md](docs/ROADMAP.md) for current progress.
