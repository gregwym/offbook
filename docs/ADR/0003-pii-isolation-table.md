# ADR 0003: PII Isolation via Dedicated Table

## Status
Accepted

## Context
The app stores sensitive financial data (account numbers, holder names, routing numbers). An AI assistant will query the database for financial context. PII must never reach the AI layer.

## Decision
Store all PII in a dedicated `pii_store` table. Main domain tables (accounts, transactions) contain only non-identifying labels and aggregated data.

## Rationale
- **Architectural enforcement** over convention: the AI service layer simply doesn't have access to `pii_repo`, so it *cannot* leak PII — not "should not" but "cannot"
- Simpler than field-level encryption: no key management, no performance overhead on every query
- Simpler than a scrubber: no regex patterns to maintain, no risk of false negatives
- Auditable: a single table to check for PII compliance
- Easy to delete all PII: truncate one table

## Consequences
- Displaying full account details requires a separate API call (`/accounts/:id/pii`)
- Joins across PII and main tables are intentionally awkward — this is a feature, not a bug
- `pii_repo` must be injected ONLY into `pii_service` — enforced by dependency injection, not by willpower
- If a new PII field is needed, add a row to `pii_store` — don't add a column to domain tables
