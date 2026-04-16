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

## Development

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full technical reference and [docs/ROADMAP.md](docs/ROADMAP.md) for current progress.
