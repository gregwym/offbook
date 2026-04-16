# ADR 0002: PostgreSQL over SQLite

## Status
Accepted

## Context
Need a database that supports exact decimal arithmetic for financial data including crypto assets (up to 18 decimal places).

## Decision
Use PostgreSQL with `NUMERIC(30, 18)` for all monetary columns.

## Rationale
- SQLite stores non-integer numbers as IEEE 754 doubles — lossy for arbitrary precision
- PostgreSQL `NUMERIC` is exact: `0.000000000000000001` stays exact through storage and arithmetic
- Crypto assets require 18 decimal places (e.g., wei in Ethereum)
- PostgreSQL also gives us: JSONB for AI context snapshots, TIMESTAMPTZ for proper timezone handling, robust concurrent access
- Docker Compose makes PostgreSQL zero-effort to run locally

## Consequences
- Requires a running PostgreSQL instance (Docker handles this)
- Slightly more complex setup than SQLite (connection string, health checks)
- In Go, use `shopspring/decimal` for all monetary arithmetic — never `float64`
- Self-hosting requires Docker or a PostgreSQL installation
