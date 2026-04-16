# ADR 0004: Plaid Integration Before CSV/PDF Import

## Status
Accepted

## Context
The app needs to ingest financial data from bank accounts. Two approaches: (1) Plaid API for direct bank integration, (2) CSV/PDF statement upload and parsing.

## Decision
Implement Plaid sandbox integration first (M3). Add CSV/PDF parsing in a later milestone.

## Rationale
- Plaid provides structured, normalized data — no parsing ambiguity
- Plaid handles deduplication via stable transaction IDs
- CSV/PDF parsing is significantly more complex: column mapping heuristics, date format detection, amount sign conventions, PDF table extraction, bank-specific formats
- Starting with Plaid establishes the data model and ingestion patterns cleanly
- CSV/PDF parsers can then conform to the same `StatementIngester` interface

## Consequences
- Need Plaid developer account and sandbox API keys for M3
- Users without Plaid can only use manual entry until CSV/PDF is added
- The `StatementIngester` interface should be designed during M3 to accommodate future CSV/PDF sources
