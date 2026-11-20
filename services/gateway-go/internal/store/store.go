// Package store is the gateway's storage layer: PostgreSQL for coaches, keys,
// agent preferences and share links; MongoDB for plans, their screenshots and
// agent runs; Redis for plan progress events.
//
// All three databases are required in every environment. A database error is
// returned to the caller; there is no in-memory or on-disk substitute. In-memory
// fakes live only in the test suites.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	// ErrCoachNotFound is returned when a coach ID has no account.
	ErrCoachNotFound = errors.New("coach not found")
	// ErrInvalidCredentials is returned when the email is unknown or the password is wrong.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrEmailTaken is returned when registering an email that already has an account.
	ErrEmailTaken = errors.New("email already registered")
	// ErrInvalidInput wraps validation failures that are the caller's fault.
	ErrInvalidInput = errors.New("invalid input")
	// ErrKeyNotFound is returned when a key ID does not exist for the coach.
	ErrKeyNotFound = errors.New("api key not found")
	// ErrPlanNotFound is returned when a plan ID has no document.
	ErrPlanNotFound = errors.New("plan not found")
	// ErrLinkNotFound is returned when a share code is unknown or expired.
	ErrLinkNotFound = errors.New("shared link expired or not found")
)

// AgentRoles lists the roles that can have their own model routing, in display order.
var AgentRoles = []string{"global", "parser", "warmup", "scaling", "injury", "timeline"}

const (
	plansCollection  = "plans"
	imagesCollection = "plan_images"
	runsCollection   = "agent_runs"
	// The Redis names agents-python publishes to.
	eventsChannel  = "session:events:"
	eventsBuffer   = "session:buffered_events:"
	eventsTTL      = 10 * time.Minute
	connectTimeout = 10 * time.Second
)

// Config holds the connection settings. All fields are required except MongoDB,
// which defaults to "coachbuddy".
type Config struct {
	PostgresDSN string
	MongoURI    string
	MongoDB     string
	RedisURL    string
	// Models is the provider and model list (internal/modelcatalog). Required.
	Models *modelcatalog.Catalog
}

type DataStore struct {
	vault  *crypto.Vault
	models *modelcatalog.Catalog
	pg     *pgxpool.Pool
	mongo  *mongo.Client
	db     *mongo.Database
	redis  *redis.Client
}

// Open connects to all three databases and pings each one. It returns an error
// naming the first database that cannot be reached; the gateway does not start
// without them.
func Open(ctx context.Context, vault *crypto.Vault, cfg Config) (*DataStore, error) {
	if cfg.PostgresDSN == "" || cfg.MongoURI == "" || cfg.RedisURL == "" {
		return nil, errors.New("store: PostgresDSN, MongoURI and RedisURL are required")
	}
	if cfg.Models == nil {
		return nil, errors.New("store: the model list is required")
	}
	if cfg.MongoDB == "" {
		cfg.MongoDB = "coachbuddy"
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	ds := &DataStore{vault: vault, models: cfg.Models}

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	ds.pg = pool

	// DefaultDocumentM makes nested documents decode as maps instead of
	// ordered key/value lists, so a session serialises to the JSON it was saved as.
	mopts := options.Client().ApplyURI(cfg.MongoURI).
		SetBSONOptions(&options.BSONOptions{DefaultDocumentM: true})
	mc, err := mongo.Connect(ctx, mopts)
	if err != nil {
		ds.Close()
		return nil, fmt.Errorf("mongodb: %w", err)
	}
	ds.mongo = mc
	if err := mc.Ping(ctx, nil); err != nil {
		ds.Close()
		return nil, fmt.Errorf("mongodb: %w", err)
	}
	ds.db = mc.Database(cfg.MongoDB)
	for coll, keys := range map[string]bson.D{
		plansCollection: {{Key: "coachId", Value: 1}, {Key: "createdAt", Value: -1}},
		runsCollection:  {{Key: "planId", Value: 1}, {Key: "revision", Value: 1}},
	} {
		if _, err := ds.db.Collection(coll).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys}); err != nil {
			ds.Close()
			return nil, fmt.Errorf("mongodb index on %s: %w", coll, err)
		}
	}

	ropts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		ds.Close()
		return nil, fmt.Errorf("redis: %w", err)
	}
	ds.redis = redis.NewClient(ropts)
	if err := ds.redis.Ping(ctx).Err(); err != nil {
		ds.Close()
		return nil, fmt.Errorf("redis: %w", err)
	}
	return ds, nil
}

// Close releases all connections.
func (ds *DataStore) Close() {
	if ds.pg != nil {
		ds.pg.Close()
	}
	if ds.mongo != nil {
		_ = ds.mongo.Disconnect(context.Background())
	}
	if ds.redis != nil {
		_ = ds.redis.Close()
	}
}

// Ping checks each database. The map always has the keys "postgres",
// "mongodb" and "redis"; a nil value means the database answered.
func (ds *DataStore) Ping(ctx context.Context) map[string]error {
	return map[string]error{
		"postgres": ds.pg.Ping(ctx),
		"mongodb":  ds.mongo.Ping(ctx, nil),
		"redis":    ds.redis.Ping(ctx).Err(),
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isInvalidUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

// --- Coaches ---

func (ds *DataStore) CreateCoach(ctx context.Context, email, password, displayName, boxName string, defaultDuration int) (*models.Coach, error) {
	emailClean := strings.ToLower(strings.TrimSpace(email))
	if emailClean == "" || password == "" {
		return nil, fmt.Errorf("%w: email and password are required", ErrInvalidInput)
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
	c := &models.Coach{
		Email:                   emailClean,
		PasswordHash:            hash,
		DisplayName:             displayName,
		BoxName:                 boxName,
		DefaultClassDurationMin: defaultDuration,
	}
	err = ds.pg.QueryRow(ctx, `
		INSERT INTO coaches (id, email, password_hash, display_name, box_name, default_class_duration_min)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, uuid.NewString(), c.Email, c.PasswordHash, c.DisplayName, c.BoxName, c.DefaultClassDurationMin).Scan(&c.ID, &c.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create coach: %w", err)
	}
	return c, nil
}

const coachColumns = `id, email, password_hash, display_name, COALESCE(box_name, ''), default_class_duration_min, created_at`

func scanCoach(row pgx.Row) (*models.Coach, error) {
	var c models.Coach
	err := row.Scan(&c.ID, &c.Email, &c.PasswordHash, &c.DisplayName, &c.BoxName, &c.DefaultClassDurationMin, &c.CreatedAt)
	return &c, err
}

func (ds *DataStore) AuthenticateCoach(ctx context.Context, email, password string) (*models.Coach, error) {
	emailClean := strings.ToLower(strings.TrimSpace(email))
	c, err := scanCoach(ds.pg.QueryRow(ctx, `SELECT `+coachColumns+` FROM coaches WHERE LOWER(email) = $1`, emailClean))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate coach: %w", err)
	}
	if !auth.CheckPassword(c.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	return c, nil
}

func (ds *DataStore) GetCoach(ctx context.Context, coachID string) (*models.Coach, error) {
	c, err := scanCoach(ds.pg.QueryRow(ctx, `SELECT `+coachColumns+` FROM coaches WHERE id = $1`, coachID))
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return nil, ErrCoachNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get coach: %w", err)
	}
	return c, nil
}

func (ds *DataStore) UpdateCoachProfile(ctx context.Context, coachID, displayName, boxName string, defaultDuration int) (*models.Coach, error) {
	if defaultDuration <= 0 {
		defaultDuration = 60
	}
	c, err := scanCoach(ds.pg.QueryRow(ctx, `
		UPDATE coaches SET display_name = $2, box_name = $3, default_class_duration_min = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING `+coachColumns, coachID, displayName, boxName, defaultDuration))
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return nil, ErrCoachNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update coach: %w", err)
	}
	return c, nil
}

// --- BYOK encrypted API keys ---

func (ds *DataStore) SaveAPIKey(ctx context.Context, coachID, provider, rawKey string, isPrimary bool) (*models.LLMAPIKey, error) {
	cleanProvider := strings.ToLower(strings.TrimSpace(provider))
	if !ds.models.HasProvider(cleanProvider) {
		return nil, fmt.Errorf("%w: provider %q is not offered (choose one of %s)", ErrInvalidInput, cleanProvider, strings.Join(ds.models.ProviderIDs(), ", "))
	}
	ciphertext, nonce, err := ds.vault.Encrypt(coachID, cleanProvider, rawKey)
	if err != nil {
		return nil, err
	}
	k := &models.LLMAPIKey{
		CoachID:      coachID,
		Provider:     cleanProvider,
		EncryptedKey: ciphertext,
		Nonce:        nonce,
		KeyHint:      crypto.MaskKey(rawKey),
		IsPrimary:    isPrimary,
	}

	err = pgx.BeginFunc(ctx, ds.pg, func(tx pgx.Tx) error {
		if isPrimary {
			if _, err := tx.Exec(ctx, `UPDATE llm_api_keys SET is_primary = FALSE WHERE coach_id = $1`, coachID); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO llm_api_keys (id, coach_id, provider, encrypted_key, nonce, key_hint, is_primary)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (coach_id, provider)
			DO UPDATE SET encrypted_key = EXCLUDED.encrypted_key, nonce = EXCLUDED.nonce, key_hint = EXCLUDED.key_hint,
			              is_primary = EXCLUDED.is_primary, updated_at = NOW()
			RETURNING id, created_at, updated_at
		`, uuid.NewString(), k.CoachID, k.Provider, k.EncryptedKey, k.Nonce, k.KeyHint, k.IsPrimary).Scan(&k.ID, &k.CreatedAt, &k.UpdatedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("save api key: %w", err)
	}
	return k, nil
}

func (ds *DataStore) ListAPIKeys(ctx context.Context, coachID string) ([]*models.LLMAPIKey, error) {
	rows, err := ds.pg.Query(ctx, `
		SELECT id, coach_id, provider, encrypted_key, nonce, key_hint, is_primary, created_at, updated_at
		FROM llm_api_keys WHERE coach_id = $1 ORDER BY created_at DESC
	`, coachID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*models.LLMAPIKey, error) {
		var k models.LLMAPIKey
		err := row.Scan(&k.ID, &k.CoachID, &k.Provider, &k.EncryptedKey, &k.Nonce, &k.KeyHint, &k.IsPrimary, &k.CreatedAt, &k.UpdatedAt)
		return &k, err
	})
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	return list, nil
}

// DeleteAPIKey deletes a key only if it belongs to the coach.
func (ds *DataStore) DeleteAPIKey(ctx context.Context, coachID, keyID string) error {
	tag, err := ds.pg.Exec(ctx, `DELETE FROM llm_api_keys WHERE id = $1 AND coach_id = $2`, keyID, coachID)
	if isInvalidUUID(err) {
		return ErrKeyNotFound
	}
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// --- Per-agent model routing ---

// Models returns the provider and model list the gateway was started with.
func (ds *DataStore) Models() *modelcatalog.Catalog { return ds.models }

// defaultAgentPrefs is the routing for a coach who has not saved their own:
// the list's default model for every role.
func defaultAgentPrefs(c *modelcatalog.Catalog) []*models.AgentModelPref {
	def := c.Default()
	out := make([]*models.AgentModelPref, 0, len(AgentRoles))
	for _, role := range AgentRoles {
		out = append(out, &models.AgentModelPref{
			ID:              "default-" + role,
			AgentRole:       role,
			Provider:        def.Provider,
			ModelIdentifier: def.ID,
		})
	}
	return out
}

func validRole(role string) bool {
	for _, r := range AgentRoles {
		if r == role {
			return true
		}
	}
	return false
}

// GetAgentPrefs returns the coach's saved routing, or the defaults when the
// coach has saved none.
func (ds *DataStore) GetAgentPrefs(ctx context.Context, coachID string) ([]*models.AgentModelPref, error) {
	rows, err := ds.pg.Query(ctx, `
		SELECT id, coach_id, agent_role, provider, model_identifier, updated_at
		FROM agent_model_prefs WHERE coach_id = $1
	`, coachID)
	if err != nil {
		return nil, fmt.Errorf("get agent prefs: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*models.AgentModelPref, error) {
		var p models.AgentModelPref
		err := row.Scan(&p.ID, &p.CoachID, &p.AgentRole, &p.Provider, &p.ModelIdentifier, &p.UpdatedAt)
		return &p, err
	})
	if err != nil {
		return nil, fmt.Errorf("get agent prefs: %w", err)
	}
	if len(list) == 0 {
		return defaultAgentPrefs(ds.models), nil
	}
	return orderPrefs(list), nil
}

func orderPrefs(list []*models.AgentModelPref) []*models.AgentModelPref {
	byRole := make(map[string]*models.AgentModelPref, len(list))
	for _, p := range list {
		byRole[p.AgentRole] = p
	}
	out := make([]*models.AgentModelPref, 0, len(list))
	for _, r := range AgentRoles {
		if p, ok := byRole[r]; ok {
			out = append(out, p)
		}
	}
	return out
}

func (ds *DataStore) SaveAgentPrefs(ctx context.Context, coachID string, prefs []models.AgentModelPref) ([]*models.AgentModelPref, error) {
	for _, p := range prefs {
		if !validRole(p.AgentRole) {
			return nil, fmt.Errorf("%w: unknown agent role %q", ErrInvalidInput, p.AgentRole)
		}
		if !ds.models.Allows(p.Provider, p.ModelIdentifier) {
			return nil, fmt.Errorf("%w: model %q is not offered for provider %q", ErrInvalidInput, p.ModelIdentifier, p.Provider)
		}
	}
	err := pgx.BeginFunc(ctx, ds.pg, func(tx pgx.Tx) error {
		for _, p := range prefs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO agent_model_prefs (id, coach_id, agent_role, provider, model_identifier, updated_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
				ON CONFLICT (coach_id, agent_role)
				DO UPDATE SET provider = EXCLUDED.provider, model_identifier = EXCLUDED.model_identifier, updated_at = NOW()
			`, uuid.NewString(), coachID, p.AgentRole, p.Provider, p.ModelIdentifier); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save agent prefs: %w", err)
	}
	return ds.GetAgentPrefs(ctx, coachID)
}

// BuildEphemeralRoutingConfig decrypts the coach's keys in memory for a single agent run.
func (ds *DataStore) BuildEphemeralRoutingConfig(ctx context.Context, coachID string) (map[string]models.EphemeralAgentRoute, error) {
	keys, err := ds.ListAPIKeys(ctx, coachID)
	if err != nil {
		return nil, err
	}
	prefs, err := ds.GetAgentPrefs(ctx, coachID)
	if err != nil {
		return nil, err
	}
	return buildRoutes(ds.vault, ds.models, coachID, keys, prefs)
}

// buildRoutes resolves a provider, key and model for every agent role.
//
// A role uses its own saved choice, else the "global" one, when that choice is still
// in the model list and the coach has a key for its provider. Otherwise it uses the
// primary key with that provider's default model. A saved model that has left the
// list (for example groq, dropped on 2026-11-18) is therefore never sent to agents.
func buildRoutes(vault *crypto.Vault, catalog *modelcatalog.Catalog, coachID string, keys []*models.LLMAPIKey, prefs []*models.AgentModelPref) (map[string]models.EphemeralAgentRoute, error) {
	decryptedByProvider := make(map[string]string)
	var primaryProvider, primaryKey string
	for _, k := range keys {
		if !catalog.HasProvider(k.Provider) {
			continue
		}
		plain, err := vault.Decrypt(coachID, k.Provider, k.EncryptedKey, k.Nonce)
		if err != nil || plain == "" {
			continue
		}
		decryptedByProvider[k.Provider] = plain
		if primaryKey == "" || k.IsPrimary {
			primaryKey = plain
			primaryProvider = k.Provider
		}
	}
	if primaryKey == "" {
		return nil, fmt.Errorf("no valid LLM API key for coach %s (supported providers: %s)", coachID, strings.Join(catalog.ProviderIDs(), ", "))
	}
	primaryModel, _ := catalog.DefaultFor(primaryProvider)

	prefByRole := make(map[string]*models.AgentModelPref)
	for _, p := range prefs {
		prefByRole[p.AgentRole] = p
	}
	usable := func(p *models.AgentModelPref) bool {
		if p == nil || !catalog.Allows(p.Provider, p.ModelIdentifier) {
			return false
		}
		_, ok := decryptedByProvider[p.Provider]
		return ok
	}

	routes := make(map[string]models.EphemeralAgentRoute, len(AgentRoles))
	for _, role := range AgentRoles {
		route := models.EphemeralAgentRoute{Provider: primaryProvider, APIKey: primaryKey, Model: primaryModel}
		for _, p := range []*models.AgentModelPref{prefByRole[role], prefByRole["global"]} {
			if usable(p) {
				route = models.EphemeralAgentRoute{Provider: p.Provider, APIKey: decryptedByProvider[p.Provider], Model: p.ModelIdentifier}
				break
			}
		}
		routes[role] = route
	}
	return routes, nil
}

// --- Plans, screenshots and agent runs (MongoDB) ---

// CreatePlan inserts a new plan document. doc must carry "planId".
func (ds *DataStore) CreatePlan(ctx context.Context, doc map[string]interface{}) error {
	id, _ := doc["planId"].(string)
	if id == "" {
		return fmt.Errorf("%w: planId is required", ErrInvalidInput)
	}
	stored := withID(doc, id)
	if _, err := ds.db.Collection(plansCollection).InsertOne(ctx, stored); err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	return nil
}

// SavePlan replaces an existing plan. A plan deleted in the meantime is not
// brought back: the result is ErrPlanNotFound.
func (ds *DataStore) SavePlan(ctx context.Context, planID string, doc map[string]interface{}) error {
	res, err := ds.db.Collection(plansCollection).ReplaceOne(ctx, bson.M{"_id": planID}, withID(doc, planID))
	if err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrPlanNotFound
	}
	return nil
}

func (ds *DataStore) GetPlan(ctx context.Context, planID string) (map[string]interface{}, error) {
	var doc map[string]interface{}
	err := ds.db.Collection(plansCollection).FindOne(ctx, bson.M{"_id": planID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPlanNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}
	return withoutID(doc), nil
}

// ListPlans returns the coach's plans, newest first.
func (ds *DataStore) ListPlans(ctx context.Context, coachID string) ([]map[string]interface{}, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cur, err := ds.db.Collection(plansCollection).Find(ctx, bson.M{"coachId": coachID}, opts)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	list := []map[string]interface{}{}
	if err := cur.All(ctx, &list); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	for i, m := range list {
		list[i] = withoutID(m)
	}
	return list, nil
}

// DeletePlan removes a plan with its screenshot and agent runs.
func (ds *DataStore) DeletePlan(ctx context.Context, planID string) error {
	doc, err := ds.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	if input, ok := doc["input"].(map[string]interface{}); ok {
		if ref, _ := input["imageRef"].(string); ref != "" {
			if _, err := ds.db.Collection(imagesCollection).DeleteOne(ctx, bson.M{"_id": ref}); err != nil {
				return fmt.Errorf("delete plan image: %w", err)
			}
		}
	}
	if _, err := ds.db.Collection(runsCollection).DeleteMany(ctx, bson.M{"planId": planID}); err != nil {
		return fmt.Errorf("delete agent runs: %w", err)
	}
	res, err := ds.db.Collection(plansCollection).DeleteOne(ctx, bson.M{"_id": planID})
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrPlanNotFound
	}
	return nil
}

// SavePlanImage stores a screenshot once, as bytes. It is never returned by
// the plan, list or share endpoints.
func (ds *DataStore) SavePlanImage(ctx context.Context, img *models.PlanImage) error {
	_, err := ds.db.Collection(imagesCollection).InsertOne(ctx, bson.M{
		"_id":       img.Ref,
		"coachId":   img.CoachID,
		"mimeType":  img.MimeType,
		"data":      primitive.Binary{Data: img.Data},
		"bytes":     len(img.Data),
		"createdAt": time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("save plan image: %w", err)
	}
	return nil
}

// SaveAgentRuns stores what ran for one revision of a plan (the agent traces),
// kept apart from the plan so it never reaches a share link.
func (ds *DataStore) SaveAgentRuns(ctx context.Context, runs *models.AgentRuns) error {
	id := fmt.Sprintf("%s:%d", runs.PlanID, runs.Revision)
	_, err := ds.db.Collection(runsCollection).ReplaceOne(ctx, bson.M{"_id": id}, bson.M{
		"_id":                    id,
		"planId":                 runs.PlanID,
		"coachId":                runs.CoachID,
		"revision":               runs.Revision,
		"runs":                   runs.Runs,
		"orchestrationLatencyMs": runs.OrchestrationLatencyMs,
		"createdAt":              time.Now().UTC(),
	}, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save agent runs: %w", err)
	}
	return nil
}

// withID returns a copy of doc with MongoDB's _id set.
func withID(doc map[string]interface{}, id string) map[string]interface{} {
	out := make(map[string]interface{}, len(doc)+1)
	for k, v := range doc {
		out[k] = v
	}
	out["_id"] = id
	return out
}

// withoutID converts a stored document back to the plan as the contract
// describes it (no _id, plain maps and slices).
func withoutID(doc map[string]interface{}) map[string]interface{} {
	out := plainDoc(doc)
	delete(out, "_id")
	return out
}

// plainDoc converts MongoDB's primitive.M and primitive.A, at any depth, to
// map[string]interface{} and []interface{}, the types the handlers assert on.
func plainDoc(doc map[string]interface{}) map[string]interface{} {
	out, _ := plainValue(doc).(map[string]interface{})
	return out
}

func plainValue(v interface{}) interface{} {
	switch t := v.(type) {
	case primitive.M:
		return plainValue(map[string]interface{}(t))
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, x := range t {
			out[k] = plainValue(x)
		}
		return out
	case primitive.A:
		return plainValue([]interface{}(t))
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, x := range t {
			out[i] = plainValue(x)
		}
		return out
	default:
		return v
	}
}

// --- Share links (PostgreSQL) ---

func (ds *DataStore) SaveSharedLink(ctx context.Context, link *models.SharedLink) error {
	err := ds.pg.QueryRow(ctx, `
		INSERT INTO shared_links (short_code, mongo_session_id, coach_id, include_coach_notes, qr_svg, expires_at, view_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`, link.ShortCode, link.PlanID, link.CoachID, link.IncludeCoachNotes, link.QRSVG, link.ExpiresAt, link.ViewCount).Scan(&link.CreatedAt)
	if err != nil {
		return fmt.Errorf("save shared link: %w", err)
	}
	return nil
}

// GetSharedLink resolves a share code and counts the view. An unknown or
// expired code returns ErrLinkNotFound.
func (ds *DataStore) GetSharedLink(ctx context.Context, code string) (*models.SharedLink, error) {
	var link models.SharedLink
	err := ds.pg.QueryRow(ctx, `
		UPDATE shared_links SET view_count = view_count + 1
		WHERE short_code = $1 AND (expires_at IS NULL OR expires_at > NOW())
		RETURNING short_code, mongo_session_id, coach_id, include_coach_notes, COALESCE(qr_svg, ''), expires_at, view_count, created_at
	`, code).Scan(&link.ShortCode, &link.PlanID, &link.CoachID, &link.IncludeCoachNotes, &link.QRSVG, &link.ExpiresAt, &link.ViewCount, &link.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get shared link: %w", err)
	}
	return &link, nil
}

// --- Plan progress events (Redis) ---

// EventStream is the progress of one plan: the events already published,
// then the live ones. Close must be called when the reader is done.
type EventStream struct {
	Buffered []string
	Live     <-chan string
	Close    func()
}

// PublishPlanEvent adds one event to the plan's replay list and sends it live,
// the same way agents-python publishes section events.
func (ds *DataStore) PublishPlanEvent(ctx context.Context, planID string, event []byte) error {
	pipe := ds.redis.TxPipeline()
	pipe.RPush(ctx, eventsBuffer+planID, event)
	pipe.Expire(ctx, eventsBuffer+planID, eventsTTL)
	pipe.Publish(ctx, eventsChannel+planID, event)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("publish plan event: %w", err)
	}
	return nil
}

// PlanEvents subscribes to a plan's live events and then reads the buffered
// ones, so no event published in between is lost. A live event that was also
// in the buffer is dropped.
func (ds *DataStore) PlanEvents(ctx context.Context, sessionID string) (*EventStream, error) {
	pubsub := ds.redis.Subscribe(ctx, eventsChannel+sessionID)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe plan events: %w", err)
	}
	buffered, err := ds.redis.LRange(ctx, eventsBuffer+sessionID, 0, -1).Result()
	if err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("read buffered plan events: %w", err)
	}
	seen := make(map[string]bool, len(buffered))
	for _, e := range buffered {
		seen[e] = true
	}

	live := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(live)
		for msg := range pubsub.Channel() {
			if seen[msg.Payload] {
				continue
			}
			select {
			case live <- msg.Payload:
			case <-done:
				return
			}
		}
	}()
	closed := false
	return &EventStream{
		Buffered: buffered,
		Live:     live,
		Close: func() {
			if closed {
				return
			}
			closed = true
			close(done)
			_ = pubsub.Close()
		},
	}, nil
}
