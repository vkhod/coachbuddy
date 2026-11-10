package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/handlers"
	"github.com/coachbuddy/gateway-go/internal/store"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	port := envOrDefault("GATEWAY_PORT", "8080")
	jwtSecret := envOrDefault("JWT_SECRET", "coachbuddy-super-secret-jwt-signing-key-2026")
	masterKeyHex := envOrDefault("MASTER_ENCRYPTION_KEY_HEX", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	pgDSN := envOrDefault("POSTGRES_DSN", "postgres://coachbuddy:coachbuddy_secret@localhost:5432/coachbuddy?sslmode=disable")
	mongoURI := envOrDefault("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := envOrDefault("MONGO_DB", "coachbuddy")
	redisURL := envOrDefault("REDIS_URL", "redis://localhost:6379/0")
	agentsURL := envOrDefault("AGENTS_SERVICE_URL", "http://localhost:8081")
	mcpRustURL := envOrDefault("MCP_RUST_URL", "http://localhost:8082")
	shareBaseURL := envOrDefault("PUBLIC_SHARE_BASE_URL", "http://localhost:3000/s")

	vault, err := crypto.NewVault(masterKeyHex)
	if err != nil {
		log.Fatalf("Failed to initialize AES-256-GCM BYOK Vault: %v", err)
	}

	ctx := context.Background()
	ds := store.NewDataStore(ctx, vault, pgDSN, mongoURI, mongoDB, redisURL)
	tm := auth.NewTokenManager(jwtSecret)
	api := handlers.NewAPIHandler(ds, tm, agentsURL, mcpRustURL, shareBaseURL)

	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("GET /health", api.Health)
	mux.HandleFunc("POST /api/v1/auth/register", api.Register)
	mux.HandleFunc("POST /api/v1/auth/login", api.Login)
	mux.HandleFunc("GET /api/v1/share/{code}", api.ResolveShareLink)

	// Authenticated routes (with seamless demo-coach fallback if no Bearer token provided)
	authMux := http.NewServeMux()
	authMux.HandleFunc("GET /api/v1/auth/me", api.GetMe)
	authMux.HandleFunc("PUT /api/v1/auth/profile", api.UpdateProfile)

	authMux.HandleFunc("GET /api/v1/settings/keys", api.ListKeys)
	authMux.HandleFunc("POST /api/v1/settings/keys", api.SaveKey)
	authMux.HandleFunc("DELETE /api/v1/settings/keys/{id}", api.DeleteKey)

	authMux.HandleFunc("GET /api/v1/settings/agents", api.GetAgentPrefs)
	authMux.HandleFunc("PUT /api/v1/settings/agents", api.SaveAgentPrefs)

	authMux.HandleFunc("POST /api/v1/sessions/generate", api.GenerateSession)
	authMux.HandleFunc("GET /api/v1/sessions", api.ListSessions)
	authMux.HandleFunc("GET /api/v1/sessions/{id}", api.GetSession)
	authMux.HandleFunc("DELETE /api/v1/sessions/{id}", api.DeleteSession)
	authMux.HandleFunc("GET /api/v1/sessions/{id}/events", api.StreamSessionEvents)
	authMux.HandleFunc("POST /api/v1/sessions/{id}/share", api.CreateShareLink)

	mux.Handle("/api/v1/", tm.Middleware(store.DefaultDemoCoachID)(authMux))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           corsMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("CoachBuddy Go API Gateway & AES-256-GCM BYOK Vault listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Gateway server error: %v", err)
	}
}
