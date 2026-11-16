package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrCoachNotFound is returned when a coach ID has no account.
var ErrCoachNotFound = errors.New("coach not found")

type DataStore struct {
	mu          sync.RWMutex
	vault       *crypto.Vault
	pgPool      *pgxpool.Pool
	mongoClient *mongo.Client
	mongoDB     *mongo.Database
	redisClient *redis.Client

	// In-memory synchronized store (acts as fast cache + standalone fallback)
	vaultFile    string
	coaches      map[string]*models.Coach
	coachesByEm  map[string]string
	apiKeys      map[string]*models.LLMAPIKey
	agentPrefs   map[string]map[string]*models.AgentModelPref // coachID -> agentRole -> pref
	defaultPrefs map[string]*models.AgentModelPref            // agentRole -> default pref for coaches without their own
	sessions     map[string]map[string]interface{}            // sessionID -> document
	sharedLinks  map[string]*models.SharedLink
}

type diskKeyRecord struct {
	ID           string    `json:"id"`
	CoachID      string    `json:"coachId"`
	Provider     string    `json:"provider"`
	EncryptedKey []byte    `json:"encryptedKey"`
	Nonce        []byte    `json:"nonce"`
	KeyHint      string    `json:"keyHint"`
	IsPrimary    bool      `json:"isPrimary"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (ds *DataStore) loadLocalVaultFile() {
	if ds.vaultFile == "" {
		return
	}
	raw, err := os.ReadFile(ds.vaultFile)
	if err != nil {
		return
	}
	var records []diskKeyRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return
	}
	for _, r := range records {
		rec := r
		ds.apiKeys[rec.ID] = &models.LLMAPIKey{
			ID:           rec.ID,
			CoachID:      rec.CoachID,
			Provider:     rec.Provider,
			EncryptedKey: rec.EncryptedKey,
			Nonce:        rec.Nonce,
			KeyHint:      rec.KeyHint,
			IsPrimary:    rec.IsPrimary,
			CreatedAt:    rec.CreatedAt,
			UpdatedAt:    rec.UpdatedAt,
		}
	}
	if len(records) > 0 {
		log.Printf("[store] Restored %d AES-256-GCM encrypted API key(s) from %s", len(records), ds.vaultFile)
	}
}

func (ds *DataStore) saveLocalVaultFileLocked() {
	if ds.vaultFile == "" {
		return
	}
	var records []diskKeyRecord
	for _, k := range ds.apiKeys {
		records = append(records, diskKeyRecord{
			ID:           k.ID,
			CoachID:      k.CoachID,
			Provider:     k.Provider,
			EncryptedKey: k.EncryptedKey,
			Nonce:        k.Nonce,
			KeyHint:      k.KeyHint,
			IsPrimary:    k.IsPrimary,
			CreatedAt:    k.CreatedAt,
			UpdatedAt:    k.UpdatedAt,
		})
	}
	if raw, err := json.MarshalIndent(records, "", "  "); err == nil {
		_ = os.WriteFile(ds.vaultFile, raw, 0600)
	}
}

func NewDataStore(ctx context.Context, vault *crypto.Vault, pgDSN, mongoURI, mongoDBName, redisURL string) *DataStore {
	ds := &DataStore{
		vault:        vault,
		coaches:      make(map[string]*models.Coach),
		coachesByEm:  make(map[string]string),
		apiKeys:      make(map[string]*models.LLMAPIKey),
		agentPrefs:   make(map[string]map[string]*models.AgentModelPref),
		defaultPrefs: make(map[string]*models.AgentModelPref),
		sessions:     make(map[string]map[string]interface{}),
		sharedLinks:  make(map[string]*models.SharedLink),
	}

	ds.seedDefaultAgentPrefs()

	// 1. Optional PostgreSQL connection
	if pgDSN != "" {
		pgCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if pool, err := pgxpool.New(pgCtx, pgDSN); err == nil {
			if err := pool.Ping(pgCtx); err == nil {
				ds.pgPool = pool
				log.Println("[store] Connected to PostgreSQL")
			} else {
				log.Printf("[store] PostgreSQL ping failed (%v) — using resilient in-memory SQL fallback", err)
				pool.Close()
			}
		}
		if ds.pgPool == nil {
			ds.vaultFile = ".coachbuddy_vault.json"
			ds.loadLocalVaultFile()
		}
	}

	// 2. Optional MongoDB connection
	if mongoURI != "" {
		mCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if client, err := mongo.Connect(mCtx, options.Client().ApplyURI(mongoURI)); err == nil {
			if err := client.Ping(mCtx, nil); err == nil {
				ds.mongoClient = client
				if mongoDBName == "" {
					mongoDBName = "coachbuddy"
				}
				ds.mongoDB = client.Database(mongoDBName)
				log.Println("[store] Connected to MongoDB")
				_, _ = ds.mongoDB.Collection("wod_sessions").Indexes().CreateOne(mCtx, mongo.IndexModel{
					Keys: bson.D{{Key: "coachId", Value: 1}, {Key: "createdAt", Value: -1}},
				})
			} else {
				log.Printf("[store] MongoDB ping failed (%v) — using resilient in-memory document fallback", err)
			}
		}
	}

	// 3. Optional Redis connection
	if redisURL != "" {
		if opt, err := redis.ParseURL(redisURL); err == nil {
			rc := redis.NewClient(opt)
			rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if err := rc.Ping(rCtx).Err(); err == nil {
				ds.redisClient = rc
				log.Println("[store] Connected to Redis Pub/Sub & Cache")
			} else {
				log.Printf("[store] Redis ping failed (%v) — SSE will use direct stream fallback", err)
			}
		}
	}

	return ds
}

func (ds *DataStore) Redis() *redis.Client {
	return ds.redisClient
}

// seedDefaultAgentPrefs sets the model routing used for any coach who has not chosen their own.
// It seeds no coach accounts and no sample sessions.
func (ds *DataStore) seedDefaultAgentPrefs() {
	for _, role := range []string{"global", "parser", "warmup", "scaling", "timeline"} {
		ds.defaultPrefs[role] = &models.AgentModelPref{
			ID:              "default-" + role,
			AgentRole:       role,
			Provider:        "gemini",
			ModelIdentifier: "gemini/gemini-3.8-flash",
			UpdatedAt:       time.Now(),
		}
	}
}

// --- Coach Auth Methods ---

func (ds *DataStore) CreateCoach(ctx context.Context, email, password, displayName, boxName string, defaultDuration int) (*models.Coach, error) {
	emailClean := strings.ToLower(strings.TrimSpace(email))
	if emailClean == "" || password == "" {
		return nil, errors.New("email and password are required")
	}
	if defaultDuration <= 0 {
		defaultDuration = 60
	}
	if boxName == "" {
		boxName = "CrossFit Box"
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}

	coach := &models.Coach{
		ID:                      uuid.NewString(),
		Email:                   emailClean,
		PasswordHash:            hash,
		DisplayName:             displayName,
		BoxName:                 boxName,
		DefaultClassDurationMin: defaultDuration,
		CreatedAt:               time.Now(),
	}

	if ds.pgPool != nil {
		err := ds.pgPool.QueryRow(ctx, `
			INSERT INTO coaches (id, email, password_hash, display_name, box_name, default_class_duration_min)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, created_at
		`, coach.ID, coach.Email, coach.PasswordHash, coach.DisplayName, coach.BoxName, coach.DefaultClassDurationMin).Scan(&coach.ID, &coach.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("email already registered or DB error: %w", err)
		}
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	if _, exists := ds.coachesByEm[emailClean]; exists && ds.pgPool == nil {
		return nil, errors.New("email already registered")
	}
	ds.coaches[coach.ID] = coach
	ds.coachesByEm[emailClean] = coach.ID
	return coach, nil
}

func (ds *DataStore) AuthenticateCoach(ctx context.Context, email, password string) (*models.Coach, error) {
	emailClean := strings.ToLower(strings.TrimSpace(email))
	if ds.pgPool != nil {
		var c models.Coach
		err := ds.pgPool.QueryRow(ctx, `
			SELECT id, email, password_hash, display_name, box_name, default_class_duration_min, created_at
			FROM coaches WHERE LOWER(email) = $1
		`, emailClean).Scan(&c.ID, &c.Email, &c.PasswordHash, &c.DisplayName, &c.BoxName, &c.DefaultClassDurationMin, &c.CreatedAt)
		if err == nil {
			if !auth.CheckPassword(c.PasswordHash, password) {
				return nil, errors.New("invalid credentials")
			}
			return &c, nil
		}
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()
	id, ok := ds.coachesByEm[emailClean]
	if !ok {
		return nil, errors.New("invalid credentials")
	}
	c := ds.coaches[id]
	if !auth.CheckPassword(c.PasswordHash, password) {
		return nil, errors.New("invalid credentials")
	}
	return c, nil
}

func (ds *DataStore) GetCoach(ctx context.Context, coachID string) (*models.Coach, error) {
	if ds.pgPool != nil {
		var c models.Coach
		err := ds.pgPool.QueryRow(ctx, `
			SELECT id, email, password_hash, display_name, box_name, default_class_duration_min, created_at
			FROM coaches WHERE id = $1
		`, coachID).Scan(&c.ID, &c.Email, &c.PasswordHash, &c.DisplayName, &c.BoxName, &c.DefaultClassDurationMin, &c.CreatedAt)
		if err == nil {
			return &c, nil
		}
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if c, ok := ds.coaches[coachID]; ok {
		return c, nil
	}
	return nil, ErrCoachNotFound
}

func (ds *DataStore) UpdateCoachProfile(ctx context.Context, coachID, displayName, boxName string, defaultDuration int) (*models.Coach, error) {
	if defaultDuration <= 0 {
		defaultDuration = 60
	}
	if ds.pgPool != nil {
		tag, err := ds.pgPool.Exec(ctx, `
			UPDATE coaches SET display_name = $2, box_name = $3, default_class_duration_min = $4, updated_at = NOW()
			WHERE id = $1
		`, coachID, displayName, boxName, defaultDuration)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrCoachNotFound
		}
		ds.mu.Lock()
		if c, ok := ds.coaches[coachID]; ok {
			c.DisplayName, c.BoxName, c.DefaultClassDurationMin = displayName, boxName, defaultDuration
		}
		ds.mu.Unlock()
		return ds.GetCoach(ctx, coachID)
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	c, ok := ds.coaches[coachID]
	if !ok {
		return nil, ErrCoachNotFound
	}
	c.DisplayName = displayName
	c.BoxName = boxName
	c.DefaultClassDurationMin = defaultDuration
	return c, nil
}

// --- BYOK Encrypted API Key Vault Methods ---

func (ds *DataStore) SaveAPIKey(ctx context.Context, coachID, provider, rawKey string, isPrimary bool) (*models.LLMAPIKey, error) {
	cleanProvider := strings.ToLower(strings.TrimSpace(provider))
	ciphertext, nonce, err := ds.vault.Encrypt(coachID, cleanProvider, rawKey)
	if err != nil {
		return nil, err
	}
	hint := crypto.MaskKey(rawKey)

	keyRec := &models.LLMAPIKey{
		ID:           uuid.NewString(),
		CoachID:      coachID,
		Provider:     cleanProvider,
		EncryptedKey: ciphertext,
		Nonce:        nonce,
		KeyHint:      hint,
		IsPrimary:    isPrimary,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if ds.pgPool != nil {
		if isPrimary {
			_, _ = ds.pgPool.Exec(ctx, `UPDATE llm_api_keys SET is_primary = FALSE WHERE coach_id = $1`, coachID)
		}
		_, err := ds.pgPool.Exec(ctx, `
			INSERT INTO llm_api_keys (id, coach_id, provider, encrypted_key, nonce, key_hint, is_primary)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (coach_id, provider)
			DO UPDATE SET encrypted_key = EXCLUDED.encrypted_key, nonce = EXCLUDED.nonce, key_hint = EXCLUDED.key_hint, is_primary = EXCLUDED.is_primary, updated_at = NOW()
		`, keyRec.ID, keyRec.CoachID, keyRec.Provider, keyRec.EncryptedKey, keyRec.Nonce, keyRec.KeyHint, keyRec.IsPrimary)
		if err != nil {
			log.Printf("[store] PG SaveAPIKey warning: %v", err)
		}
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	if isPrimary {
		for _, k := range ds.apiKeys {
			if k.CoachID == coachID {
				k.IsPrimary = false
			}
		}
	}
	ds.apiKeys[keyRec.ID] = keyRec
	ds.saveLocalVaultFileLocked()
	return keyRec, nil
}

func (ds *DataStore) ListAPIKeys(ctx context.Context, coachID string) ([]*models.LLMAPIKey, error) {
	if ds.pgPool != nil {
		rows, err := ds.pgPool.Query(ctx, `
			SELECT id, coach_id, provider, encrypted_key, nonce, key_hint, is_primary, created_at, updated_at
			FROM llm_api_keys WHERE coach_id = $1 ORDER BY created_at DESC
		`, coachID)
		if err == nil {
			defer rows.Close()
			var list []*models.LLMAPIKey
			for rows.Next() {
				var k models.LLMAPIKey
				if err := rows.Scan(&k.ID, &k.CoachID, &k.Provider, &k.EncryptedKey, &k.Nonce, &k.KeyHint, &k.IsPrimary, &k.CreatedAt, &k.UpdatedAt); err == nil {
					list = append(list, &k)
				}
			}
			if len(list) > 0 {
				return list, nil
			}
		}
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()
	var res []*models.LLMAPIKey
	for _, k := range ds.apiKeys {
		if k.CoachID == coachID {
			res = append(res, k)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].CreatedAt.After(res[j].CreatedAt)
	})
	return res, nil
}

func (ds *DataStore) DeleteAPIKey(ctx context.Context, coachID, keyID string) error {
	if ds.pgPool != nil {
		_, _ = ds.pgPool.Exec(ctx, `DELETE FROM llm_api_keys WHERE id = $1 AND coach_id = $2`, keyID, coachID)
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.apiKeys, keyID)
	ds.saveLocalVaultFileLocked()
	return nil
}

// --- Per-Agent Model Routing Preferences ---

func (ds *DataStore) GetAgentPrefs(ctx context.Context, coachID string) ([]*models.AgentModelPref, error) {
	if ds.pgPool != nil {
		rows, err := ds.pgPool.Query(ctx, `
			SELECT id, coach_id, agent_role, provider, model_identifier, updated_at
			FROM agent_model_prefs WHERE coach_id = $1
		`, coachID)
		if err == nil {
			defer rows.Close()
			var list []*models.AgentModelPref
			for rows.Next() {
				var p models.AgentModelPref
				if err := rows.Scan(&p.ID, &p.CoachID, &p.AgentRole, &p.Provider, &p.ModelIdentifier, &p.UpdatedAt); err == nil {
					list = append(list, &p)
				}
			}
			if len(list) > 0 {
				return list, nil
			}
		}
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()
	roleMap, ok := ds.agentPrefs[coachID]
	if !ok {
		roleMap = ds.defaultPrefs
	}
	var out []*models.AgentModelPref
	order := []string{"global", "parser", "warmup", "scaling", "timeline"}
	for _, r := range order {
		if p, exists := roleMap[r]; exists {
			out = append(out, p)
		}
	}
	return out, nil
}

func (ds *DataStore) SaveAgentPrefs(ctx context.Context, coachID string, prefs []models.AgentModelPref) ([]*models.AgentModelPref, error) {
	ds.mu.Lock()
	if _, ok := ds.agentPrefs[coachID]; !ok {
		ds.agentPrefs[coachID] = make(map[string]*models.AgentModelPref)
	}
	for _, p := range prefs {
		cp := p
		if cp.ID == "" {
			cp.ID = uuid.NewString()
		}
		cp.CoachID = coachID
		cp.UpdatedAt = time.Now()
		ds.agentPrefs[coachID][cp.AgentRole] = &cp

		if ds.pgPool != nil {
			_, _ = ds.pgPool.Exec(ctx, `
				INSERT INTO agent_model_prefs (id, coach_id, agent_role, provider, model_identifier, updated_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
				ON CONFLICT (coach_id, agent_role)
				DO UPDATE SET provider = EXCLUDED.provider, model_identifier = EXCLUDED.model_identifier, updated_at = NOW()
			`, cp.ID, coachID, cp.AgentRole, cp.Provider, cp.ModelIdentifier)
		}
	}
	ds.mu.Unlock()
	return ds.GetAgentPrefs(ctx, coachID)
}

// BuildEphemeralRoutingConfig decrypts the coach's AES-256-GCM keys in-memory for a single agent run.
func (ds *DataStore) BuildEphemeralRoutingConfig(ctx context.Context, coachID string) (map[string]models.EphemeralAgentRoute, error) {
	keys, _ := ds.ListAPIKeys(ctx, coachID)
	prefs, _ := ds.GetAgentPrefs(ctx, coachID)

	// =========================================================================
	// TODO(engineer) 2.2: Ephemeral Routing Config Key Decryption & Fallback
	//
	// Invariants to enforce:
	// 1. Decrypt each key with:
	//    `plain, err := ds.vault.Decrypt(coachID, k.Provider, k.EncryptedKey, k.Nonce)`
	//    Store in `decryptedByProvider[k.Provider] = plain`.
	//    Track `primaryKey` and `primaryProvider` (from `k.IsPrimary == true`, or first decrypted key).
	// 2. If no keys could be decrypted (or keys is empty):
	//    Return nil, fmt.Errorf("no valid LLM API keys configured for coach %s", coachID)
	// 3. For each role in ["global", "parser", "warmup", "scaling", "timeline"]:
	//    - Check if coach configured a specific pref for that role.
	//    - If pref specified provider, use `decryptedByProvider[pref.Provider]`, falling back to `primaryKey`.
	//    - If no key could be resolved for a role, return nil, fmt.Errorf("missing key for provider %s", targetProvider)
	// =========================================================================
	decryptedByProvider := make(map[string]string)
	var primaryProvider, primaryKey string

	for _, k := range keys {
		plain, err := ds.vault.Decrypt(coachID, k.Provider, k.EncryptedKey, k.Nonce)
		if err != nil || plain == "" {
			continue
		}
		decryptedByProvider[k.Provider] = plain
		if primaryKey == "" || k.IsPrimary == true {
			primaryKey = plain
			primaryProvider = k.Provider
		}
	}

	if primaryKey == "" {
		return nil, fmt.Errorf("no valid LLM API keys configured for coach %s", coachID)
	}

	prefByRole := make(map[string]*models.AgentModelPref)
	for _, p := range prefs {
		prefByRole[p.AgentRole] = p
	}

	ephemeralRoutes := make(map[string]models.EphemeralAgentRoute)

	roles := []string{"global", "parser", "warmup", "scaling", "timeline"}
	for _, role := range roles {
		pref, prefExists := prefByRole[role]
		providerToUse := primaryProvider
		modelToUse := "gemini/gemini-3.8-flash"
		if prefExists && pref.Provider != "" {
			providerToUse = pref.Provider
			modelToUse = pref.ModelIdentifier
		}

		keyToUse, hasKey := decryptedByProvider[providerToUse]
		if !hasKey {
			providerToUse = primaryProvider
			keyToUse = primaryKey
			if globalPref, ok := prefByRole["global"]; ok && globalPref.Provider == primaryProvider && globalPref.ModelIdentifier != "" {
				modelToUse = globalPref.ModelIdentifier
			} else {
				switch primaryProvider {
				case "openai":
					modelToUse = "openai/gpt-4o"
				case "anthropic":
					modelToUse = "anthropic/claude-3-7-sonnet-latest"
				case "groq":
					modelToUse = "groq/llama-3.3-70b-versatile"
				default:
					modelToUse = "gemini/gemini-3.8-flash"
				}
			}
		}

		ephemeralRoutes[role] = models.EphemeralAgentRoute{
			Provider: providerToUse,
			APIKey:   keyToUse,
			Model:    modelToUse,
		}

	}
	return ephemeralRoutes, nil
}

// --- WOD Session Document Store (MongoDB + In-Memory Sync) ---

func (ds *DataStore) SaveSession(ctx context.Context, sessionID string, doc map[string]interface{}) error {
	doc["_id"] = sessionID
	doc["sessionId"] = sessionID

	if ds.mongoDB != nil {
		coll := ds.mongoDB.Collection("wod_sessions")
		opts := options.Replace().SetUpsert(true)
		_, err := coll.ReplaceOne(ctx, bson.M{"_id": sessionID}, doc, opts)
		if err != nil {
			log.Printf("[store] MongoDB ReplaceOne warning: %v", err)
		}
	}

	// Deep copy via JSON into in-memory map
	raw, _ := json.Marshal(doc)
	var copyDoc map[string]interface{}
	_ = json.Unmarshal(raw, &copyDoc)

	ds.mu.Lock()
	ds.sessions[sessionID] = copyDoc
	ds.mu.Unlock()
	return nil
}

func (ds *DataStore) GetSession(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	if ds.mongoDB != nil {
		coll := ds.mongoDB.Collection("wod_sessions")
		var res map[string]interface{}
		if err := coll.FindOne(ctx, bson.M{"_id": sessionID}).Decode(&res); err == nil {
			res["sessionId"] = sessionID
			return res, nil
		}
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if doc, ok := ds.sessions[sessionID]; ok {
		return doc, nil
	}
	return nil, errors.New("session not found")
}

func (ds *DataStore) ListSessions(ctx context.Context, coachID string) ([]map[string]interface{}, error) {
	ds.mu.RLock()
	var list []map[string]interface{}
	seen := make(map[string]bool)
	for id, doc := range ds.sessions {
		if fmt.Sprintf("%v", doc["coachId"]) == coachID {
			seen[id] = true
			list = append(list, doc)
		}
	}
	ds.mu.RUnlock()

	if ds.mongoDB != nil {
		coll := ds.mongoDB.Collection("wod_sessions")
		cur, err := coll.Find(ctx, bson.M{"coachId": coachID})
		if err == nil {
			defer cur.Close(ctx)
			for cur.Next(ctx) {
				var m map[string]interface{}
				if err := cur.Decode(&m); err == nil {
					idStr := fmt.Sprintf("%v", m["_id"])
					m["sessionId"] = idStr
					if !seen[idStr] {
						seen[idStr] = true
						list = append(list, m)
					} else {
						// Replace with latest from Mongo
						for idx, existing := range list {
							if fmt.Sprintf("%v", existing["sessionId"]) == idStr {
								list[idx] = m
								break
							}
						}
					}
				}
			}
		}
	}

	// Strip heavy imageBase64 from list view to avoid massive network transfers
	for _, item := range list {
		if input, ok := item["input"].(map[string]interface{}); ok {
			delete(input, "imageBase64")
		}
	}

	sort.Slice(list, func(i, j int) bool {
		ti := fmt.Sprintf("%v", list[i]["createdAt"])
		tj := fmt.Sprintf("%v", list[j]["createdAt"])
		return ti > tj
	})
	return list, nil
}

func (ds *DataStore) DeleteSession(ctx context.Context, sessionID string) error {
	if ds.mongoDB != nil {
		_, _ = ds.mongoDB.Collection("wod_sessions").DeleteOne(ctx, bson.M{"_id": sessionID})
	}
	ds.mu.Lock()
	delete(ds.sessions, sessionID)
	ds.mu.Unlock()
	return nil
}

// --- Shared Links & QR Codes ---

func (ds *DataStore) SaveSharedLink(ctx context.Context, link *models.SharedLink) error {
	if ds.pgPool != nil {
		_, _ = ds.pgPool.Exec(ctx, `
			INSERT INTO shared_links (short_code, mongo_session_id, coach_id, include_coach_notes, qr_svg, expires_at, view_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (short_code) DO UPDATE SET qr_svg = EXCLUDED.qr_svg, include_coach_notes = EXCLUDED.include_coach_notes, expires_at = EXCLUDED.expires_at
		`, link.ShortCode, link.MongoSessionID, link.CoachID, link.IncludeCoachNotes, link.QRSVG, link.ExpiresAt, link.ViewCount)
	}
	if ds.redisClient != nil {
		data, _ := json.Marshal(link)
		ttl := 24 * time.Hour
		if link.ExpiresAt != nil {
			rem := time.Until(*link.ExpiresAt)
			if rem > 0 {
				ttl = rem
			}
		}
		_ = ds.redisClient.Set(ctx, "share:"+link.ShortCode, data, ttl).Err()
	}
	ds.mu.Lock()
	ds.sharedLinks[link.ShortCode] = link
	ds.mu.Unlock()
	return nil
}

func (ds *DataStore) GetSharedLink(ctx context.Context, code string) (*models.SharedLink, error) {
	if ds.redisClient != nil {
		if val, err := ds.redisClient.Get(ctx, "share:"+code).Result(); err == nil && val != "" {
			var link models.SharedLink
			if err := json.Unmarshal([]byte(val), &link); err == nil {
				if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
					return nil, errors.New("shared link expired")
				}
				return &link, nil
			}
		}
	}

	if ds.pgPool != nil {
		var link models.SharedLink
		err := ds.pgPool.QueryRow(ctx, `
			UPDATE shared_links SET view_count = view_count + 1
			WHERE short_code = $1
			RETURNING short_code, mongo_session_id, coach_id, include_coach_notes, COALESCE(qr_svg, ''), expires_at, view_count, created_at
		`, code).Scan(&link.ShortCode, &link.MongoSessionID, &link.CoachID, &link.IncludeCoachNotes, &link.QRSVG, &link.ExpiresAt, &link.ViewCount, &link.CreatedAt)
		if err == nil {
			if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
				return nil, errors.New("shared link expired")
			}
			return &link, nil
		}
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	if link, ok := ds.sharedLinks[code]; ok {
		if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
			return nil, errors.New("shared link expired")
		}
		link.ViewCount++
		return link, nil
	}
	return nil, errors.New("shared link not found")
}
