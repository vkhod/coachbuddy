# Rust Domain MCP Server — Agent Rules (`services/mcp-rust`)

This directory contains `mcp-rust` (`:8082`), a high-performance, deterministic **Model Context Protocol (MCP)** server built with **Rust (Axum + Tokio)**.

---

## 1. Directory Layout

- `src/main.rs`: startup. Requires `MONGO_URI` and a successful MongoDB ping; `MCP_ALLOWED_HOSTS` sets the `Host` allow-list for `/mcp`. `coachbuddy-mcp-rust healthcheck` is the container health check.
- `src/app.rs`: Axum router (`/health`, `/mcp`, `/tools/history`, `/tools/timeline`, `/tools/qr`).
- `src/tests.rs`: router tests (MCP protocol, REST, health); the real-MongoDB test runs when `MCP_TEST_MONGO_URI` is set (CI sets it).
- `src/contract.rs`: tool input and output types generated from `schema/tools.schema.json` by `scripts/gen-schema` (do not edit); `src/contract_tests.rs` checks the shared examples.
- `src/mcp.rs`: the `rmcp` `ServerHandler` (tool list and `tools/call`), tool definitions, and `execute_named_tool`, shared with the REST routes. rmcp serves the protocol over Streamable HTTP, stateless, with JSON responses.
- `src/domain/history.rs`: `get_recent_coaching_history` implementation querying MongoDB `plans` (the collection the gateway writes) for recent warm-up drills to prevent repetition.
- `src/domain/timeline.rs`: `verify_timeline_math` pure deterministic verifier checking `0..totalMinutes` contiguity, positive block durations, gaps/overlaps, and exact duration sum.
- `src/domain/qr.rs`: `generate_qr_svg` zero-external-asset SVG QR code generator with custom quiet zone and high-contrast styling.

---

## 2. Key Guardrails & Conventions

1. **Deterministic & Allocation-Light Domain Logic**:
   - Keep `src/domain/timeline.rs` and `src/domain/qr.rs` pure, deterministic, and covered by unit tests.
   - Never mutate coach notes or labels during timeline math verification—only report `issues`, `discrepancyMinutes`, `feasibilityStatus` (`OPTIMAL`, `TIGHT`, `INVALID`), and `coachWarnings`.
2. **MongoDB Required**:
   - `MONGO_URI` is required in every environment; the server exits without it. A failed or timed-out history query (8 s limit) is a tool result with `isError: true` over MCP and a 503 over REST, never an empty history.
   - Never log connection strings with credentials; use `redact_uri` in `src/main.rs`.
3. **MCP Errors**:
   - Unknown tool: protocol error (`McpError::invalid_params`, `-32602`). Bad arguments or a failure while running: `CallToolResult::error` with the reason, so the calling agent can read it.
   - Never return substitute output (no placeholder QR code, no empty history on failure).
4. **Dual Interface Parity**:
   - Whenever adding or updating a tool in `src/domain/`, expose it through both the MCP JSON-RPC handler (`src/mcp.rs`) and the direct internal REST routes (`src/main.rs`), using `#[serde(rename_all = "camelCase")]` on input/output structs.

---

## 3. Commands

```bash
# Run unit & MCP protocol tests (the real-MongoDB test skips without MCP_TEST_MONGO_URI)
cargo test
MCP_TEST_MONGO_URI=mongodb://localhost:27017 cargo test

# Run the MCP server locally on :8082
cargo run
```
