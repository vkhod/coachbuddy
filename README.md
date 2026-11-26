# CoachBuddy — Polyglot Multi-Agent CrossFit Coaching Platform

**CoachBuddy** builds a class plan for CrossFit and functional fitness coaches from a photo or screenshot of the WOD: a warm-up for the body parts the WOD uses, scaling tiers for every part (RX, Scaled, Senior, and RX+ on engine parts), rows for injured and other athletes who need attention today, and a minute-by-minute timeline. It is a phone app (PWA) on a polyglot set of services and a multi-agent pipeline. Each coach brings their own LLM key; there is no demo or offline plan builder.

Architecture & System Design documentation: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

---

## Polyglot Technology Stack

| Layer | Service Directory | Technology | Core Responsibilities |
| :--- | :--- | :--- | :--- |
| **Frontend PWA** | `web/pwa-nextjs` | **TypeScript, Next.js 16 (App Router), React 19, Tailwind CSS** | Capture, the plan in class order with section-by-section progress, changes and versions, history, Settings (key, model per agent, agent runs), public share view (`/s/[code]`), box branding. See its [README](web/pwa-nextjs/README.md). |
| **API Gateway & Key Vault** | `services/gateway-go` | **Go 1.27, PostgreSQL (`pgx`), MongoDB, Redis** | JWT Auth, **AES-256-GCM BYOK API Key Encryption**, Per-Agent Model Routing, Redis SSE Event Streaming, Short-Link & QR Share management. |
| **Multi-Agent Orchestrator** | `services/agents-python` | **Python 3.12+, FastAPI, Google ADK, LiteLLM** | ADK `Workflow`: `WodParserAgent`, then `WarmupPlannerAgent` and `ScalingAgent` in parallel, then `InjuryAgent` (when the comment names injuries) and `TimelineAgent`; one `LiteLlm` per agent with the coach's key; mcp-rust tools through `McpToolset`. |
| **Domain MCP Server** | `services/mcp-rust` | **Rust, Axum, MCP Streamable HTTP (rmcp)** | Deterministic MCP tools: `get_recent_coaching_history`, `verify_timeline_math` and `generate_qr_svg`. |
| **Data Layer** | `compose.yml` | **PostgreSQL 16, MongoDB 7, Redis 7** | Polyglot persistence: relational user/vault store (Postgres), hierarchical WOD session history (MongoDB), and real-time event pub/sub (Redis). |

---

## Quick Start

### Option A: Full Polyglot Stack via Docker Compose
```bash
cp .env.example .env
# Fill in every secret in .env; generate each one with: openssl rand -hex 32
docker compose -f compose.yml -f compose.dev.yml up --build
```
`compose.yml` publishes no ports. `compose.dev.yml` publishes them on `127.0.0.1` for local development only.
- **CoachBuddy PWA**: http://localhost:3000
- **Go API Gateway**: http://localhost:8080/health
- **Python Google ADK Service**: http://localhost:8081/health
- **Rust MCP Server**: http://localhost:8082/health

Sign up in the app, then add a Gemini, OpenAI or Anthropic key on the welcome screen; plans use it. To run the app under a box's name and logo, see [config/branding/README.md](config/branding/README.md).

### Option B: Running a Single Service on the Host
Services need their settings from the environment (see `.env.example`); there are no default secrets. The databases come from Option A. The gateway creates and updates the Postgres tables itself when it starts (`services/gateway-go/internal/migrations/`).

1. **Go API Gateway (`:8080`)**:
   ```bash
   cd services/gateway-go
   go test ./...
   go run ./cmd/gateway
   ```
2. **Python Google ADK Orchestrator (`:8081`)**:
   ```bash
   cd services/agents-python
   pip install --require-hashes -r requirements.txt
   uvicorn app.main:app --port 8081 --reload
   ```
3. **Next.js Coach PWA (`:3000`)**:
   ```bash
   cd web/pwa-nextjs
   npm install
   npm run dev
   ```
