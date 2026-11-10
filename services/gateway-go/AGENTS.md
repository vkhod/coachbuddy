# Go API Gateway & BYOK Security Vault — Agent Rules (`services/gateway-go`)

This directory contains `gateway-go` (`:8080`), the Go 1.23+ API Gateway, JWT Auth provider, AES-256-GCM BYOK Key Vault, Redis SSE streamer, and public share coordinator.

---

## 1. Directory Layout

- `cmd/gateway/main.go`: Entry point, HTTP server configuration, route registration, and CORS middleware.
- `internal/auth/`: JWT (`HS256`) token generation, verification, and auth middleware.
- `internal/crypto/`: `Vault` implementation (`vault.go`, `vault_test.go`) performing AES-256-GCM envelope encryption/decryption and key masking.
- `internal/handlers/`: HTTP handlers for Auth, BYOK Vault/Routing settings, WOD Session generation, SSE streaming, History, and Public QR Share links (`/api/v1/share/{code}`).
- `internal/models/`: Request/response DTOs and domain structures (`camelCase` JSON tags).
- `internal/store/`: Dual-mode storage layer supporting PostgreSQL (`pgx`), MongoDB, and Redis with automatic in-memory fallback when databases are offline.

---

## 2. Key Guardrails & Conventions

1. **Envelope Encryption (`internal/crypto/vault.go`)**:
   - Always generate a cryptographically random 12-byte nonce (`crypto/rand`) per encryption operation.
   - Never return decrypted keys in any HTTP response; use `MaskAPIKey` before serializing `ProviderKeyStatus` or `VaultSettings`.
   - Decrypted keys may only be attached to internal outbound requests to `agents-python` (`:8081`).
2. **Resilient Storage & Upstream Fallbacks**:
   - Preserve graceful degradation in `internal/store/` and `internal/handlers/` when Postgres, MongoDB, Redis, `agents-python`, or `mcp-rust` are unreachable in local standalone mode.
3. **SSE Streaming**:
   - Ensure SSE endpoints set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and flush via `http.Flusher` on each event.

---

## 3. Commands

```bash
# Run all unit & vault crypto tests
go test -v ./...

# Start the gateway locally on :8080
go run ./cmd/gateway
```
