# CoachBuddy — Workspace Agent Rules (`AGENTS.md`)

**CoachBuddy** is a polyglot microservices & multi-agent CrossFit coaching platform (WOD preparation, 2-phase non-repetitive warm-ups, 4-tier movement scaling, minute-by-minute class timelines, live gym-floor timer, and QR code session sharing).

For deeper architectural rationale and system diagrams, consult [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## 1. Polyglot Repository Map & Hierarchical Rules

Each service directory contains its own scoped `AGENTS.md` with stack-specific conventions and commands:

| Service / Layer | Path | Host Port | Tech Stack | Scoped Rules |
| :--- | :--- | :--- | :--- | :--- |
| **Coach PWA & Share UI** | `web/pwa-nextjs` | `:3000` | TypeScript, Next.js 14 (App Router), Tailwind CSS | [web/pwa-nextjs/AGENTS.md](web/pwa-nextjs/AGENTS.md) |
| **API Gateway & Key Vault** | `services/gateway-go` | `:8080` | Go 1.27, PostgreSQL (`pgx`), MongoDB, Redis | [services/gateway-go/AGENTS.md](services/gateway-go/AGENTS.md) |
| **Multi-Agent Orchestrator** | `services/agents-python` | `:8081` | Python 3.12+, FastAPI, Google ADK, LiteLLM | [services/agents-python/AGENTS.md](services/agents-python/AGENTS.md) |
| **Domain MCP Server** | `services/mcp-rust` | `:8082` | Rust, Axum, Tokio, MCP (`JSON-RPC 2.0`) | [services/mcp-rust/AGENTS.md](services/mcp-rust/AGENTS.md) |
| **Data & Infra** | `compose.yml`, `compose.dev.yml`, `infra/` | `:5432`, `:27017`, `:6379` | PostgreSQL 16, MongoDB 7, Redis 7 | — |

---

## 2. Core Cross-Service Invariants (Always Follow)

1. **Same Behaviour in Every Environment (No Canned Plans)**:
   - Development and production run the same code paths. A plan is only ever produced by the real agents calling a real LLM with a real key. There is no zero-key, demo or hybrid mode.
   - Never substitute canned, regex-generated or locally re-computed content when an LLM call, an MCP tool call or another service fails. Mark the affected step or plan section as failed, with the reason, so the coach can retry.
   - Fakes belong only in automated tests: recorded LLM responses (fixtures) and test doubles inside the test suites, never in application code paths.
   - The current code still contains such fallbacks (regex parser and canned content in `agents-python`, local MCP re-implementation in `app/mcp_client.py`, local plan builder in `web/pwa-nextjs/src/lib/api.ts`). They are removed during the redesign; do not add new ones.
   - The databases (Postgres, MongoDB, Redis) are required in every environment; `compose.yml` provides them locally. The existing in-memory store fallback in `gateway-go` and the DB-less cold start in `mcp-rust` are removed during the redesign; do not add new ones. In-memory fakes belong only in tests.
   - Secrets have no defaults in code or compose files. Services refuse to start when a required secret is missing.
2. **BYOK API Key Security (`AES-256-GCM`)**:
   - User LLM keys (`gemini`, `openai`, `anthropic`, `groq`) are encrypted at rest in `gateway-go` using AES-256-GCM with unique 12-byte nonces.
   - Never log, persist in plaintext, or return raw API keys to the frontend—always return masked previews (`sk-p••••9x8Z`) and only pass decrypted keys ephemerally over internal requests to `agents-python`.
3. **Cross-Service Contract Alignment**:
   - JSON payloads exchanged between `pwa-nextjs` $\leftrightarrow$ `gateway-go` $\leftrightarrow$ `agents-python` $\leftrightarrow$ `mcp-rust` use `camelCase` field names (`coachId`, `totalMinutes`, `startMin`, `endMin`, `feasibilityStatus`, etc.). Keep Go structs, Python Pydantic models, Rust Serde structs, and TypeScript interfaces synchronized when modifying schemas.
4. **Agent Customization Maintenance**:
   - When adding a new microservice, MCP tool, or major architectural pattern, update this root `AGENTS.md` and the corresponding service-level `AGENTS.md`.

---

## 3. Common Build & Verification Commands

- **Full Stack (Docker Compose)** (needs a filled-in `.env`, see `.env.example`):
  ```bash
  docker compose -f compose.yml -f compose.dev.yml up --build
  ```
- **Go Gateway (`services/gateway-go`)**:
  ```bash
  cd services/gateway-go && go test -v ./...
  ```
- **Python Orchestrator (`services/agents-python`)**:
  ```bash
  cd services/agents-python && pip install --require-hashes -r requirements.txt -r requirements-dev.txt && pytest -v
  ```
- **Rust MCP Server (`services/mcp-rust`)**:
  ```bash
  cd services/mcp-rust && cargo fmt --check && cargo test --locked
  ```
- **Next.js PWA (`web/pwa-nextjs`)**:
  ```bash
  cd web/pwa-nextjs && npm run build
  ```
