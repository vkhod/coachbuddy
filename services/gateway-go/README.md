# CoachBuddy — Go API Gateway & AES-256-GCM BYOK Key Vault (`gateway-go`)

High-concurrency API Gateway written in **Go**, handling:
- **Coach Authentication**: JWT (`HS256`) registration, login, and profile management stored in **PostgreSQL**.
- **BYOK (Bring Your Own Key) Security Vault**: **AES-256-GCM** envelope encryption (`internal/crypto/vault.go`) with random 12-byte nonces per key, masking keys (`sk-p••••9x8Z`) on all API responses, and decrypting keys strictly in-memory for ephemeral dispatch to the Python Google ADK Multi-Agent service.
- **Per-Agent LLM Routing**: Allows coaches to assign a global LLM provider/model or customize individual agents (`parser`, `warmup`, `scaling`, `timeline`).
- **Real-time SSE Proxy**: Subscribes to **Redis Pub/Sub** (`session:events:{id}`) to stream multi-agent progress to the Next.js PWA.
- **Short-Link & QR Code Sharing**: Coordinates with `mcp-rust` (`POST /tools/qr`) to generate crisp SVG QR codes and serves public athlete/co-coach views (`GET /api/v1/share/{code}`).
