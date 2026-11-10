# CoachBuddy — Workspace Agent Rules (`AGENTS.md`)

**CoachBuddy** is a polyglot microservices & multi-agent CrossFit coaching platform (WOD preparation, 2-phase non-repetitive warm-ups, 4-tier movement scaling, minute-by-minute class timelines, live gym-floor timer, and QR code session sharing).

For deeper architectural rationale and system diagrams, consult [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## 1. Polyglot Repository Map & Hierarchical Rules

Each service directory contains its own scoped `AGENTS.md` with stack-specific conventions and commands:

| Service / Layer | Path | Host Port | Tech Stack | Scoped Rules |
| :--- | :--- | :--- | :--- | :--- |
| **Coach PWA & Share UI** | `web/pwa-nextjs` | `:3000` | TypeScript, Next.js 14 (App Router), Tailwind CSS | [web/pwa-nextjs/AGENTS.md](web/pwa-nextjs/AGENTS.md) |
| **API Gateway & Key Vault** | `services/gateway-go` | `:8080` | Go 1.23+, PostgreSQL (`pgx`), MongoDB, Redis | [services/gateway-go/AGENTS.md](services/gateway-go/AGENTS.md) |
| **Multi-Agent Orchestrator** | `services/agents-python` | `:8081` | Python 3.12+, FastAPI, Google ADK, LiteLLM | [services/agents-python/AGENTS.md](services/agents-python/AGENTS.md) |
| **Domain MCP Server** | `services/mcp-rust` | `:8082` | Rust, Axum, Tokio, MCP (`JSON-RPC 2.0`) | [services/mcp-rust/AGENTS.md](services/mcp-rust/AGENTS.md) |
| **Data & Infra** | `docker-compose.yml`, `infra/` | `:5432`, `:27017`, `:6379` | PostgreSQL 16, MongoDB 7, Redis 7 | — |

---

## 2. Core Cross-Service Invariants (Always Follow)

1. **Resilient Standalone / Zero-Key Execution**:
   - Every microservice must remain runnable and testable on its own even if Postgres, MongoDB, Redis, or external LLM keys are absent.
   - Do not break in-memory store fallbacks in `gateway-go`, deterministic MCP/regex fallbacks in `agents-python`, or DB-less cold-start fallbacks in `mcp-rust`.
2. **BYOK API Key Security (`AES-256-GCM`)**:
   - User LLM keys (`gemini`, `openai`, `anthropic`, `groq`) are encrypted at rest in `gateway-go` using AES-256-GCM with unique 12-byte nonces.
   - Never log, persist in plaintext, or return raw API keys to the frontend—always return masked previews (`sk-p••••9x8Z`) and only pass decrypted keys ephemerally over internal requests to `agents-python`.
3. **Cross-Service Contract Alignment**:
   - JSON payloads exchanged between `pwa-nextjs` $\leftrightarrow$ `gateway-go` $\leftrightarrow$ `agents-python` $\leftrightarrow$ `mcp-rust` use `camelCase` field names (`coachId`, `totalMinutes`, `startMin`, `endMin`, `feasibilityStatus`, etc.). Keep Go structs, Python Pydantic models, Rust Serde structs, and TypeScript interfaces synchronized when modifying schemas.
4. **Agent Customization Maintenance**:
   - When adding a new microservice, MCP tool, or major architectural pattern, update this root `AGENTS.md` and the corresponding service-level `AGENTS.md`.

---

## 3. Common Build & Verification Commands

- **Full Stack (Docker Compose)**:
  ```bash
  docker compose up --build
  ```
- **Go Gateway (`services/gateway-go`)**:
  ```bash
  cd services/gateway-go && go test -v ./...
  ```
- **Python Orchestrator (`services/agents-python`)**:
  ```bash
  cd services/agents-python && pytest -v
  ```
- **Rust MCP Server (`services/mcp-rust`)**:
  ```bash
  cd services/mcp-rust && cargo test
  ```
- **Next.js PWA (`web/pwa-nextjs`)**:
  ```bash
  cd web/pwa-nextjs && npm run build
  ```
