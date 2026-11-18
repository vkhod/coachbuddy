# Go API Gateway & BYOK Security Vault — Agent Rules (`services/gateway-go`)

This directory contains `gateway-go` (`:8080`), the Go 1.27 API Gateway, JWT Auth provider, AES-256-GCM BYOK Key Vault, Redis SSE streamer, and public share coordinator.

---

## 1. Directory Layout

- `cmd/gateway/main.go`: Entry point, HTTP server configuration, route registration, and CORS middleware.
- `internal/auth/`: JWT (`HS256`) token generation, verification, and auth middleware.
- `internal/crypto/`: `Vault` implementation (`vault.go`, `vault_test.go`) performing AES-256-GCM envelope encryption/decryption and key masking.
- `internal/handlers/`: HTTP handlers for Auth, BYOK Vault/Routing settings, WOD Session generation, SSE streaming, History, and Public QR Share links (`/api/v1/share/{code}`).
- `internal/contract/`: generated from `schema/` by `scripts/gen-schema` (do not edit); `contract_test.go` checks the shared examples.
- `internal/models/`: Request/response DTOs and domain structures (`camelCase` JSON tags).
- `internal/store/`: Storage on PostgreSQL (`pgx`: coaches, keys, agent preferences, share links), MongoDB (session documents) and Redis (session progress events). All three are required; `store.Open` fails if one is unreachable. `integration_test.go` runs against real databases when the `GATEWAY_TEST_*` variables are set (CI sets them).

---

## 2. Key Guardrails & Conventions

1. **Envelope Encryption (`internal/crypto/vault.go`)**:
   - Always generate a cryptographically random 12-byte nonce (`crypto/rand`) per encryption operation.
   - Never return decrypted keys in any HTTP response; use `MaskAPIKey` before serializing `ProviderKeyStatus` or `VaultSettings`.
   - Decrypted keys may only be attached to internal outbound requests to `agents-python` (`:8081`).
2. **Storage and Upstream Failures**:
   - If `agents-python` or `mcp-rust` is unreachable or returns an error, return the error to the client (for example `502` with a clear message). Never build or substitute plan content in the gateway.
   - Postgres, MongoDB and Redis are required in every environment. A database error is returned (500, or 503 for the event stream), never replaced by in-memory data. The only in-memory store is the fake in `internal/handlers/fake_store_test.go`.
   - The QR code comes only from mcp-rust (`/tools/qr`). If it fails, creating a share link answers 502 and saves nothing.
   - `GET /health` checks Postgres, MongoDB, Redis and mcp-rust and answers 503 if any is down. The body names the failing dependency; the reason goes to the log only.
   - Handlers depend on the `handlers.Store` interface, implemented by `*store.DataStore`.
   - Every authenticated route requires a valid token (no demo coach), and every session, share and key operation checks that the record belongs to the caller.
3. **SSE Streaming**:
   - Ensure SSE endpoints set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and flush via `http.Flusher` on each event.

---

## 3. Commands

```bash
# Run all unit & vault crypto tests (integration tests skip without databases)
go test -v ./...

# Include the store integration tests against local databases
GATEWAY_TEST_POSTGRES_DSN='postgres://...' GATEWAY_TEST_MONGO_URI='mongodb://...' \
GATEWAY_TEST_REDIS_URL='redis://...' go test ./internal/store/

# Start the gateway locally on :8080
go run ./cmd/gateway
```
