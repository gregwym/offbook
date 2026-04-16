# ADR 0001: Go for Backend

## Status
Accepted

## Context
Need a backend language for a privacy-first finance app. Key requirements: Plaid SDK support, strong typing, fast builds, easy deployment (single binary), mature HTTP ecosystem.

## Decision
Use Go with the Gin HTTP framework.

## Rationale
- Single binary deployment — ideal for self-hosting (no runtime deps)
- Excellent concurrency model for handling Plaid webhooks and AI streaming
- Official Plaid Go SDK (`plaid-go`)
- Strong typing catches financial calculation errors at compile time
- Gin is the most widely used Go web framework with good middleware ecosystem
- Fast compilation enables tight development loops

## Consequences
- No class-based OOP — use interfaces and composition instead
- Error handling is explicit (no exceptions) — more verbose but predictable
- GORM has quirks compared to mature ORMs (ActiveRecord, SQLAlchemy) — accept the trade-off for Go's other benefits
