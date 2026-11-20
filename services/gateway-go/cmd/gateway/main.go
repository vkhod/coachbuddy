package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/handlers"
	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
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

// requireEnv returns the value of a required setting or stops the gateway.
// Secrets and connection strings have no defaults in any environment.
func requireEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("Required environment variable %s is not set (see .env.example)", key)
	}
	return v
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	port := envOrDefault("GATEWAY_PORT", "8080")
	jwtSecret := requireEnv("JWT_SECRET")
	masterKeyHex := requireEnv("MASTER_ENCRYPTION_KEY_HEX")
	internalToken := requireEnv("INTERNAL_GATEWAY_TOKEN")
	pgDSN := requireEnv("POSTGRES_DSN")
	mongoURI := requireEnv("MONGO_URI")
	mongoDB := envOrDefault("MONGO_DB", "coachbuddy")
	redisURL := requireEnv("REDIS_URL")
	agentsURL := envOrDefault("AGENTS_SERVICE_URL", "http://localhost:8081")
	mcpRustURL := envOrDefault("MCP_RUST_URL", "http://localhost:8082")
	shareBaseURL := envOrDefault("PUBLIC_SHARE_BASE_URL", "http://localhost:3000/s")

	// The provider and model list: embedded, or MODELS_FILE to replace it without a rebuild.
	catalog, err := modelcatalog.Load(os.Getenv("MODELS_FILE"))
	if err != nil {
		log.Fatalf("Invalid model list: %v", err)
	}

	vault, err := crypto.NewVault(masterKeyHex)
	if err != nil {
		log.Fatalf("Failed to initialize AES-256-GCM BYOK Vault: %v", err)
	}

	// The databases are required in every environment: stop if any is unreachable.
	ds, err := store.Open(context.Background(), vault, store.Config{
		PostgresDSN: pgDSN,
		MongoURI:    mongoURI,
		MongoDB:     mongoDB,
		RedisURL:    redisURL,
		Models:      catalog,
	})
	if err != nil {
		log.Fatalf("Cannot start without the databases: %v", err)
	}
	defer ds.Close()
	tm, err := auth.NewTokenManager(jwtSecret)
	if err != nil {
		log.Fatalf("Invalid JWT_SECRET: %v", err)
	}
	api := handlers.NewAPIHandler(ds, tm, agentsURL, mcpRustURL, shareBaseURL, internalToken)

	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("GET /health", api.Health)
	mux.HandleFunc("POST /api/v1/auth/register", api.Register)
	mux.HandleFunc("POST /api/v1/auth/login", api.Login)
	mux.HandleFunc("GET /api/v1/share/{code}", api.ResolveShareLink)

	// Authenticated routes: a valid token is required
	authMux := http.NewServeMux()
	authMux.HandleFunc("GET /api/v1/auth/me", api.GetMe)
	authMux.HandleFunc("PUT /api/v1/auth/profile", api.UpdateProfile)

	authMux.HandleFunc("GET /api/v1/settings/keys", api.ListKeys)
	authMux.HandleFunc("POST /api/v1/settings/keys", api.SaveKey)
	authMux.HandleFunc("DELETE /api/v1/settings/keys/{id}", api.DeleteKey)

	authMux.HandleFunc("GET /api/v1/settings/models", api.ListModels)
	authMux.HandleFunc("GET /api/v1/settings/agents", api.GetAgentPrefs)
	authMux.HandleFunc("PUT /api/v1/settings/agents", api.SaveAgentPrefs)

	authMux.HandleFunc("POST /api/v1/plans", api.CreatePlan)
	authMux.HandleFunc("GET /api/v1/plans", api.ListPlans)
	authMux.HandleFunc("GET /api/v1/plans/{id}", api.GetPlan)
	authMux.HandleFunc("DELETE /api/v1/plans/{id}", api.DeletePlan)
	authMux.HandleFunc("GET /api/v1/plans/{id}/events", api.StreamPlanEvents)
	authMux.HandleFunc("POST /api/v1/plans/{id}/share", api.CreateShareLink)

	mux.Handle("/api/v1/", tm.Middleware()(authMux))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           corsMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// On SIGTERM (docker stop): stop taking requests, then let running plan
	// generations record themselves as failed instead of staying pending.
	stop, stopCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopCancel()
	go func() {
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("CoachBuddy Go API Gateway & AES-256-GCM BYOK Vault listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Gateway server error: %v", err)
	}
	<-stop.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := api.Shutdown(ctx); err != nil {
		log.Printf("Plan generations still running at shutdown: %v", err)
	}
}
