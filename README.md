# CoachBuddy — Polyglot Multi-Agent CrossFit Coaching Platform

**CoachBuddy** is an intelligent WOD preparation, timeline management, warm-up gamification, and 4-tier movement scaling Progressive Web App (PWA) built for CrossFit & functional fitness coaches—architected as a **production-grade polyglot microservices & multi-agent system**.

Architecture & System Design documentation: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

---

## Polyglot Technology Stack

| Layer | Service Directory | Technology | Core Responsibilities |
| :--- | :--- | :--- | :--- |
| **Frontend PWA** | `web/pwa-nextjs` | **TypeScript, Next.js (App Router), Tailwind CSS** | WOD Planner, Live Agent Stepper, 4-Tier Scaling Matrix, **Coach Floor Mode** (live class timer + `WakeLock`), BYOK Vault UI, Public QR Share View (`/s/[code]`). |
| **API Gateway & Key Vault** | `services/gateway-go` | **Go 1.27, PostgreSQL (`pgx`), MongoDB, Redis** | JWT Auth, **AES-256-GCM BYOK API Key Encryption**, Per-Agent Model Routing, Redis SSE Event Streaming, Short-Link & QR Share management. |
| **Multi-Agent Orchestrator** | `services/agents-python` | **Python 3.12+, FastAPI, Google ADK, LiteLLM** | Hierarchical `SequentialAgent` + `ParallelAgent` pipeline (`WodParserAgent`, `WarmupPlannerAgent`, `ScalingAndCuesAgent`, `TimelineLogisticsAgent`) with dynamic BYOK routing and MCP tool execution. |
| **Domain MCP Server** | `services/mcp-rust` | **Rust, Axum, MCP Streamable HTTP (rmcp)** | Deterministic MCP tools: `get_recent_coaching_history`, `verify_timeline_math` and `generate_qr_svg`. |
| **Data Layer** | `docker-compose.yml` | **PostgreSQL 16, MongoDB 7, Redis 7** | Polyglot persistence: relational user/vault store (Postgres), hierarchical WOD session history (MongoDB), and real-time event pub/sub (Redis). |

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

### Option B: Running a Single Service on the Host
Services need their settings from the environment (see `.env.example`); there are no default secrets. The databases come from Option A.

1. **Go API Gateway (`:8080`)**:
   ```bash
   cd services/gateway-go
   go test ./...
   go run ./cmd/gateway
   ```
2. **Python Google ADK Orchestrator (`:8081`)**:
   ```bash
   cd services/agents-python
   pip install -r requirements.txt
   uvicorn app.main:app --port 8081 --reload
   ```
3. **Next.js Coach PWA (`:3000`)**:
   ```bash
   cd web/pwa-nextjs
   npm install
   npm run dev
   ```
