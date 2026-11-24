# CoachBuddy — Go API Gateway & AES-256-GCM BYOK Key Vault (`gateway-go`)

High-concurrency API Gateway written in **Go**, handling:
- **Coach Authentication**: JWT (`HS256`) registration, login, and profile management stored in **PostgreSQL**.
- **BYOK (Bring Your Own Key) Security Vault**: **AES-256-GCM** envelope encryption (`internal/crypto/vault.go`) with random 12-byte nonces per key, masking keys (`sk-p••••9x8Z`) on all API responses, and decrypting keys strictly in-memory for ephemeral dispatch to the Python Google ADK Multi-Agent service.
- **Per-Agent LLM Routing**: a coach picks one model for all agents or one per agent (`parser`, `warmup`, `scaling`, `injury`, `timeline`) from `internal/modelcatalog/models.json` (`GET /api/v1/settings/models`, `GET` and `PUT /api/v1/settings/agents`). `GET /api/v1/settings/runs` lists the agent runs per revision.
- **Real-time SSE Proxy**: Subscribes to **Redis Pub/Sub** (`session:events:{id}`) to stream plan progress, section by section, to the Next.js PWA (`GET /api/v1/plans/{id}/events`). Sections can be regenerated with an instruction; each change is a revision that can be restored.
- **Short-Link & QR Code Sharing**: Coordinates with `mcp-rust` (`POST /tools/qr`) to generate crisp SVG QR codes and serves public athlete/co-coach views (`GET /api/v1/share/{code}`).
