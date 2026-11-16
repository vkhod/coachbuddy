# Rust Domain MCP Server — Agent Rules (`services/mcp-rust`)

This directory contains `mcp-rust` (`:8082`), a high-performance, deterministic **Model Context Protocol (MCP)** server built with **Rust (Axum + Tokio)**.

---

## 1. Directory Layout

- `src/main.rs`: Axum router (`/health`, `/mcp`, `/tools/history`, `/tools/timeline`, `/tools/qr`), optional MongoDB connection pool (`AppState`), and server startup.
- `src/mcp.rs`: JSON-RPC 2.0 MCP protocol dispatcher (`initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`) and JSON Schema tool definitions.
- `src/domain/history.rs`: `get_recent_coaching_history` implementation querying MongoDB `wod_sessions` for recent warm-up drills to prevent repetition.
- `src/domain/timeline.rs`: `verify_timeline_math` pure deterministic verifier checking `0..totalMinutes` contiguity, positive block durations, gaps/overlaps, and exact duration sum.
- `src/domain/qr.rs`: `generate_qr_svg` zero-external-asset SVG QR code generator with custom quiet zone and high-contrast styling.

---

## 2. Key Guardrails & Conventions

1. **Deterministic & Allocation-Light Domain Logic**:
   - Keep `src/domain/timeline.rs` and `src/domain/qr.rs` pure, deterministic, and covered by unit tests.
   - Never mutate coach notes or labels during timeline math verification—only report `issues`, `discrepancyMinutes`, `feasibilityStatus` (`OPTIMAL`, `TIGHT`, `INVALID`), and `coachWarnings`.
2. **MongoDB Required**:
   - `MONGO_URI` is required in every environment. The current DB-less cold start (`AppState.db = None`, empty `recentWarmups`) is removed during the redesign; a failed history query must surface as an MCP error, not as an empty result.
   - Never log connection strings with credentials; use `redact_uri` in `src/main.rs`.
3. **Dual Interface Parity**:
   - Whenever adding or updating a tool in `src/domain/`, expose it through both the MCP JSON-RPC handler (`src/mcp.rs`) and the direct internal REST routes (`src/main.rs`), using `#[serde(rename_all = "camelCase")]` on input/output structs.

---

## 3. Commands

```bash
# Run unit & MCP protocol tests
cargo test

# Run the MCP server locally on :8082
cargo run
```
