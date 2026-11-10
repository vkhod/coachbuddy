# CoachBuddy — Rust Domain MCP Server (`mcp-rust`)

High-performance, deterministic **Model Context Protocol (MCP)** server written in **Rust (Axum + Tokio)** exposing MongoDB coaching history queries, pure timeline math validation, and high-contrast SVG QR code generation.

## Endpoints

### 1. Health Check
- `GET /health` -> Returns service status, version (`0.1.0`), MongoDB connection status, and registered MCP tool names.

### 2. Model Context Protocol (MCP JSON-RPC 2.0)
- `POST /mcp`
  - `initialize`: Returns MCP protocol version (`2024-11-05`), `tools` capability, and `serverInfo`.
  - `tools/list`: Returns JSON Schema definitions for the 3 deterministic domain tools.
  - `tools/call`: Executes any of the 3 tools and returns both MCP `content` (`[{"type": "text", "text": "..."}]`) and `structuredContent`.

### 3. Direct Internal REST Endpoints
For sub-millisecond internal microservice calls from `gateway-go` and `agents-python`:
- `POST /tools/history` (`get_recent_coaching_history`)
- `POST /tools/timeline` (`verify_timeline_math`)
- `POST /tools/qr` (`generate_qr_svg`)
