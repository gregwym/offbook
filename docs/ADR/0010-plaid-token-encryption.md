# ADR 0010: Plaid `access_token` Encryption at Rest

## Status
Accepted

## Context
Plaid's `access_token` is a long-lived bearer credential. Whoever holds it can call `/transactions/sync`, `/accounts/get`, and every other Plaid Item endpoint on behalf of the user — i.e. read every transaction in every linked bank account. It is materially more sensitive than a password hash (a hash is not directly usable; an `access_token` is).

`SESSION_SECRET` is already in the environment for HMACing session tokens. The cheapest path would be to reuse it as the key for AES-GCM. The dearer path is a dedicated `PLAID_TOKEN_KEY` that can be rotated, revoked, or stored differently (e.g. in a sealed secret) without disrupting sessions.

The privacy-first stance of the project ([ADR-0003](0003-pii-isolation-table.md)) and the planned move to self-hosted instances mean we should assume operators may run on shared boxes, may snapshot the DB into less-controlled places, and may not encrypt at the filesystem layer. The `access_token` MUST NOT survive in plaintext in any of those paths.

## Decision
1. **Dedicated key.** `PLAID_TOKEN_KEY` is a separate, required env var holding a 32-byte (hex-encoded, 64 char) AES-256 key.
2. **AES-GCM.** Per-row random 12-byte nonce, prepended to the ciphertext. Authenticated encryption — tampering is detected at decrypt time.
3. **Versioned ciphertext.** Stored bytes start with a single-byte version (`0x01` today) so a future key/scheme rotation can decrypt old rows without a flag day.
4. **Hard-required at startup when Plaid is configured.** If `PLAID_CLIENT_ID` is set but `PLAID_TOKEN_KEY` is missing/short, `config.Load()` fails fast — the server refuses to start rather than silently fall back to plaintext storage.
5. **Storage column is `access_token_enc BYTEA`.** Naming makes it obvious nothing should be read out as a string and used directly.

## Rationale
- **Separate key keeps blast radius bounded.** A leaked `SESSION_SECRET` invalidates every session (annoying, recoverable in seconds). A leaked Plaid token key exposes every linked bank account (catastrophic). Coupling them means one leak is two leaks.
- **AES-GCM is the default authenticated cipher in Go's stdlib (`crypto/cipher`).** No third-party dep.
- **Random nonce per row** is the standard pattern; nonce reuse with the same key in GCM is a key-compromise event, so generating fresh nonces and prefixing them eliminates the foot-gun.
- **Version byte** is cheap insurance. We do not need to rotate today, but the cost to skip it and need it later is rewriting every stored row.
- **Fail-fast at startup** beats discovering at exchange time that we cannot write an `access_token`. The check belongs in `config.Load()` next to other required-var guards.

## Consequences
- `internal/crypto/token.go` owns `Encrypt(plaintext []byte) ([]byte, error)` and `Decrypt(ciphertext []byte) ([]byte, error)`. Constructor takes the raw key bytes; key parsing and validation happen in `config.Load()`.
- `plaid_items.access_token_enc` is `BYTEA`. No view/SELECT returns it to the API surface — it never leaves `plaid_service`.
- New env var documented in `.env.example` and `docs/ARCHITECTURE.md`. Operators must `openssl rand -hex 32` to generate one.
- The encryption helper is generic; future surfaces that need at-rest secrets (refresh tokens for other providers, API keys for AI providers) can reuse it.
- Rotation: when needed, generate a new key, re-encrypt every row in a one-shot job, swap `PLAID_TOKEN_KEY`. Version byte makes the dual-key window straightforward to implement.

## Alternatives Considered
- **Reuse `SESSION_SECRET`.** Rejected — see blast-radius point above.
- **NaCl secretbox** (`golang.org/x/crypto/nacl/secretbox`). Equivalent security; chose AES-GCM because it's stdlib-only.
- **pgcrypto column encryption.** Pushes the key into the DB — defeats the purpose of encrypting against DB-snapshot exposure.
- **Store plaintext, file backlog for later.** Rejected. The first M3 commit that wires Plaid Link is the moment real bearer tokens land in the DB. Encrypting after the fact requires a backfill and a key-rotation story we will have to build *anyway*. Doing it on day one is the same code with no migration cost.

## Follow-up
- When a second at-rest-secret surface lands (e.g. AI provider API keys), generalize `internal/crypto/token.go` from `plaid`-specific naming to a shared `secretbox` package. Not needed today.
- Rotation runner (`cmd/plaid-rekey`) — defer until rotation is actually required.
