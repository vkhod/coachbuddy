# Go API Gateway & BYOK Security Vault — Agent Rules (`services/gateway-go`)

This directory contains `gateway-go` (`:8080`), the Go 1.27 API Gateway, JWT Auth provider, AES-256-GCM BYOK Key Vault, Redis SSE streamer, and public share coordinator.

---

## 1. Directory Layout

- `cmd/gateway/main.go`: Entry point, HTTP server configuration, route registration, and CORS middleware.
- `internal/auth/`: JWT (`HS256`) token generation, verification, and auth middleware.
- `internal/crypto/`: `Vault` implementation (`vault.go`, `vault_test.go`) performing AES-256-GCM envelope encryption/decryption and key masking.
- `internal/handlers/`: HTTP handlers. `handlers.go`: health, auth, BYOK vault and routing settings, the mcp-rust QR call. `plans.go`: asynchronous plan generation, plan read/list/delete, the SSE stream. `share.go`: share links and the athlete and coach views (`/api/v1/share/{code}`).
- `internal/contract/`: generated from `schema/` by `scripts/gen-schema` (do not edit); `contract_test.go` checks the shared examples.
- `internal/models/`: Request/response DTOs and domain structures (`camelCase` JSON tags).
- `internal/store/`: Storage on PostgreSQL (`pgx`: coaches, keys, agent preferences, share links), MongoDB (`plans`, `plan_images` for the screenshots, `agent_runs` for the traces) and Redis (plan progress events). All three are required; `store.Open` fails if one is unreachable. `integration_test.go` runs against real databases when the `GATEWAY_TEST_*` variables are set (CI sets them).

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
3. **Plan generation (asynchronous, Phase 2 step 10)**:
   - `POST /api/v1/plans` validates the request against the contract (`contract.CreatePlanRequest`, plus a check for unknown fields), stores the screenshot under `input.imageRef`, saves the plan with its owner and every section `pending`, answers `202 {planId}`, and calls agents-python in the background (`GenerateTimeout`, 5 minutes).
   - agents-python publishes one `PlanEvent` per section to Redis, each carrying the plan as it stands. The gateway stores each valid snapshot as progress (only the gateway writes plan documents), then saves the final plan, moves `agentRuns` to `agent_runs`, and publishes the closing `plan` event. The owner and `createdAt` are always the gateway's.
   - Any failure (agents unreachable, an error status, a timeout, a plan that does not match the contract, a shutdown) marks every unfinished section `failed` with the reason and publishes `plan` `failed`. Sections that finished are kept. A plan deleted during generation stays deleted.
   - `Shutdown` (called on SIGTERM) cancels running generations and waits until they have recorded their failure, so no plan stays `pending`.
4. **SSE Streaming**:
   - `GET /api/v1/plans/{id}/events` checks the owner before the stream opens, replays the buffered events and ends at the `plan` event. For a finished plan whose events have expired it sends one closing event built from the stored plan.
   - Ensure SSE endpoints set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and flush via `http.Flusher` on each event.
5. **Share links**:
   - Both views are built by copying named fields (`share.go`), so a new plan field stays private until added there on purpose. `input` (comment, screenshot reference), section errors and agent runs are never shared. The athlete view has the WOD, the tiers and the timeline; the coach view adds the warm-up, `today`, injuries, cues, details and timeline warnings. A new link is the athlete view unless the request says `{"includeCoachNotes": true}`.

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
