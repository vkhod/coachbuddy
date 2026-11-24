# Go API Gateway & BYOK Security Vault — Agent Rules (`services/gateway-go`)

This directory contains `gateway-go` (`:8080`), the Go 1.27 API Gateway, JWT Auth provider, AES-256-GCM BYOK Key Vault, Redis SSE streamer, and public share coordinator.

---

## 1. Directory Layout

- `cmd/gateway/main.go`: Entry point, HTTP server configuration, route registration, and CORS middleware.
- `internal/auth/`: JWT (`HS256`) token generation, verification, and auth middleware.
- `internal/crypto/`: `Vault` implementation (`vault.go`, `vault_test.go`) performing AES-256-GCM envelope encryption/decryption and key masking.
- `internal/handlers/`: HTTP handlers. `handlers.go`: health, auth, BYOK vault and routing settings, the mcp-rust QR call. `plans.go`: asynchronous plan generation, plan read/list/delete, the SSE stream. `revisions.go`: section regeneration, the revision list, reading and restoring a revision. `share.go`: share links and the athlete and coach views (`/api/v1/share/{code}`).
- `internal/contract/`: generated from `schema/` by `scripts/gen-schema` (do not edit); `contract_test.go` checks the shared examples.
- `internal/models/`: Request/response DTOs and domain structures (`camelCase` JSON tags).
- `internal/store/`: Storage on PostgreSQL (`pgx`: coaches, keys, agent preferences, share links), MongoDB (`plans`, `plan_images` for the screenshots, `agent_runs` for the traces, `plan_revisions` with one record per revision) and Redis (plan progress events). All three are required; `store.Open` fails if one is unreachable. `integration_test.go` runs against real databases when the `GATEWAY_TEST_*` variables are set (CI sets them).

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
   - Handlers depend on the `handlers.Store` interface, implemented by `*store.DataStore`. The test fake (`fake_store_test.go`) can fail per call: `fail`, `createPlanErr`, `savePlanErr`, `eventsErr`, `pingErrs`.
   - MongoDB decodes nested documents as `primitive.M` / `primitive.A`, which fail a `.(map[string]interface{})` assertion. Pass every document read from MongoDB through `plainDoc` (`internal/store/store.go`) first.
   - The generated Go request types do not reject unknown fields; handlers that decode a contract request add a strict decode.
   - Every authenticated route requires a valid token (no demo coach), and every session, share and key operation checks that the record belongs to the caller.
3. **Plan generation (asynchronous, Phase 2 step 10)**:
   - `POST /api/v1/plans` validates the request against the contract (`contract.CreatePlanRequest`, plus a check for unknown fields), stores the screenshot under `input.imageRef`, saves the plan with its owner and every section `pending`, answers `202 {planId}`, and calls agents-python in the background (`GenerateTimeout`, 5 minutes).
   - agents-python publishes one `PlanEvent` per section to Redis, each carrying the plan as it stands. The gateway stores each valid snapshot as progress (only the gateway writes plan documents), then saves the final plan, moves `agentRuns` to `agent_runs`, and publishes the closing `plan` event. The owner and `createdAt` are always the gateway's.
   - Any failure (agents unreachable, an error status, a timeout, a plan that does not match the contract, a shutdown) marks every unfinished section `failed` with the reason and publishes `plan` `failed`. Sections that finished are kept; when the warm-up is not done, a kept timeline is marked `provisional` (the same rule as agents-python). A plan deleted during generation stays deleted.
   - `Shutdown` (called on SIGTERM) cancels running generations and waits until they have recorded their failure, so no plan stays `pending`.
4. **Refinement and versions (Phase 2 step 11)**:
   - `POST /api/v1/plans/{id}/sections/{section}/regenerate` (`warmup`, `parts`, `injuries`, `timeline`; never `wod`) validates `contract.RegenerateSectionRequest` strictly, needs a finished plan whose WOD was read (409 otherwise), moves the plan to revision n+1 with the section `pending` and its content removed, answers 202, and calls agents-python's `/api/v1/regenerate` in the background with the plan as it was at n. Same job as a new plan (`planJob`, `generate`).
   - `GET /api/v1/plans/{id}/revisions` lists `contract.PlanRevision` entries (change, sections built, state); `GET .../revisions/{n}` returns the plan as it was; `POST .../revisions/{n}/restore` copies it into a new revision, without a model call, and publishes a closing `plan` event.
   - Every change is a new revision; nothing is overwritten. A failed regeneration is a revision with that section failed; the earlier one can be restored.
   - One change at a time: `AdvancePlan` moves a plan only from the revision the change started from (409 otherwise), and `SavePlan` writes only while the plan is still at the document's revision, so an older job cannot overwrite a newer revision.
   - Agent runs are stored per revision (`agent_runs` `_id` = `planId:revision`). Deleting a plan deletes its revisions.
5. **SSE Streaming**:
   - `GET /api/v1/plans/{id}/events` checks the owner before the stream opens, replays the buffered events of the plan's current revision (older ones stay in the replay list and are skipped) and ends at the `plan` event. For a finished plan whose events have expired it sends one closing event built from the stored plan.
   - Ensure SSE endpoints set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and flush via `http.Flusher` on each event.
6. **Share links**:
   - Both views are built by copying named fields (`share.go`), so a new plan field stays private until added there on purpose. `input` (comment, screenshot reference), section errors and agent runs are never shared. The athlete view has the WOD, the tiers and the timeline; the coach view adds the warm-up, `today`, injuries, cues, details and timeline warnings. A new link is the athlete view unless the request says `{"includeCoachNotes": true}`.

---

## 3. Commands

```bash
# Run all unit & vault crypto tests (integration tests skip without databases)
go test -v ./...

# Include the store integration tests against local databases
GATEWAY_TEST_POSTGRES_DSN='postgres://...' GATEWAY_TEST_MONGO_URI='mongodb://...' \
GATEWAY_TEST_REDIS_URL='redis://...' go test ./internal/store/

# The store integration tests drop the Postgres tables and the MongoDB database they run against:
# never point them at a database another check or a running gateway is using.

# Start the gateway locally on :8080
go run ./cmd/gateway
```
