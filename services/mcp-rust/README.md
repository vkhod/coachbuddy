# CoachBuddy — Rust Domain MCP Server (`mcp-rust`)

Deterministic **Model Context Protocol (MCP)** server in **Rust (Axum + Tokio + rmcp)**: MongoDB coaching history, timeline math verification, and SVG QR codes.

MongoDB is required. Without `MONGO_URI`, or if the first ping fails, the server exits.

## Endpoints

### 1. Health
- `GET /health`: 200 when MongoDB answers a ping, 503 otherwise (`checks.mongodb`). The container health check runs `coachbuddy-mcp-rust healthcheck`, which calls this route.

### 2. MCP (Streamable HTTP, `POST /mcp`)
Served by [`rmcp`](https://crates.io/crates/rmcp), the official Rust MCP SDK, in stateless mode with JSON responses:
- The protocol version is negotiated (`2024-11-05` up to `2026-07-28`).
- Clients must send `Accept: application/json, text/event-stream` (otherwise 406). The `Host` header must be in `MCP_ALLOWED_HOSTS` (default `localhost,127.0.0.1,::1,mcp-rust`; otherwise 403).
- Notifications such as `notifications/initialized` get `202` with no body. `GET /mcp` is 405 (no server-initiated stream).
- `tools/call` returns `content` and `structuredContent`. Bad arguments and failures while running a tool (for example MongoDB down) come back as `isError: true` with the reason. An unknown tool is a JSON-RPC error `-32602`.

### 3. Internal REST
For `gateway-go`: 200 with the result, 400 for bad arguments, 503 when the tool fails.
- `POST /tools/history` (`get_recent_coaching_history`)
- `POST /tools/timeline` (`verify_timeline_math`)
- `POST /tools/qr` (`generate_qr_svg`; empty data or data too long for a QR code is 400, never a placeholder image)
