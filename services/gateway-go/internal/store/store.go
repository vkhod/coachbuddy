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

const DefaultDemoCoachID = "00000000-0000-0000-0000-000000000001"

type DataStore struct {
	mu          sync.RWMutex
	vault       *crypto.Vault
	pgPool      *pgxpool.Pool
	mongoClient *mongo.Client
	mongoDB     *mongo.Database
	redisClient *redis.Client

	// In-memory synchronized store (acts as fast cache + standalone fallback)
	vaultFile   string
	coaches     map[string]*models.Coach
	coachesByEm map[string]string
	apiKeys     map[string]*models.LLMAPIKey
	agentPrefs  map[string]map[string]*models.AgentModelPref // coachID -> agentRole -> pref
	sessions    map[string]map[string]interface{}            // sessionID -> document
	sharedLinks map[string]*models.SharedLink
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
		vault:       vault,
		coaches:     make(map[string]*models.Coach),
		coachesByEm: make(map[string]string),
		apiKeys:     make(map[string]*models.LLMAPIKey),
		agentPrefs:  make(map[string]map[string]*models.AgentModelPref),
		sessions:    make(map[string]map[string]interface{}),
		sharedLinks: make(map[string]*models.SharedLink),
	}

	ds.seedDemoData()

	// 1. Optional PostgreSQL connection
	if pgDSN != "" {
		pgCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if pool, err := pgxpool.New(pgCtx, pgDSN); err == nil {
			if err := pool.Ping(pgCtx); err == nil {
				ds.pgPool = pool
				log.Println("[store] Connected to PostgreSQL")
				ds.ensureDemoCoachInPostgres(pgCtx)
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

func (ds *DataStore) seedDemoData() {
	hash, _ := auth.HashPassword("coachbuddy123")
	demoCoach := &models.Coach{
		ID:                      DefaultDemoCoachID,
		Email:                   "coach@crossfitbox.com",
		PasswordHash:            hash,
		DisplayName:             "Coach Alex (Fill-In)",
		BoxName:                 "CrossFit Iron Horizon",
		DefaultClassDurationMin: 60,
		CreatedAt:               time.Now().Add(-24 * time.Hour),
	}
	ds.coaches[demoCoach.ID] = demoCoach
	ds.coachesByEm[strings.ToLower(demoCoach.Email)] = demoCoach.ID

	roles := []struct {
		role  string
		model string
	}{
		{"global", "gemini/gemini-3.8-flash"},
		{"parser", "gemini/gemini-3.8-flash"},
		{"warmup", "gemini/gemini-3.8-flash"},
		{"scaling", "gemini/gemini-3.8-flash"},
		{"timeline", "gemini/gemini-3.8-flash"},
	}
	ds.agentPrefs[DefaultDemoCoachID] = make(map[string]*models.AgentModelPref)
	for _, r := range roles {
		ds.agentPrefs[DefaultDemoCoachID][r.role] = &models.AgentModelPref{
			ID:              uuid.NewString(),
			CoachID:         DefaultDemoCoachID,
			AgentRole:       r.role,
			Provider:        "gemini",
			ModelIdentifier: r.model,
			UpdatedAt:       time.Now(),
		}
	}

	// Seed a rich sample WOD session so History & Share work out of the box
	sampleID := "session-seed-broomstick-thrusters"
	ds.sessions[sampleID] = map[string]interface{}{
		"_id":       sampleID,
		"sessionId": sampleID,
		"coachId":   DefaultDemoCoachID,
		"status":    "completed",
		"title":     "Heavy Front Squat 3RM + Thruster & Pull-Up Engine",
		"createdAt": time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		"input": map[string]interface{}{
			"rawText":               "Part A (Strength - 15 min): Find a Heavy 3-Rep Max Front Squat from the rack.\nPart B (Engine / Metcon - 12 min cap): 21-15-9 reps for time of Thrusters (95/65 lb) and Pull-Ups.",
			"coachConstraints":      "Today I want to use mostly a broomstick (PVC) for the warm-up and start with a fun reaction game to wake everyone up.",
			"targetDurationMinutes": 60,
			"equipmentFocus":        []string{"pvc_broomstick", "barbell", "pullup_rig"},
		},
		"parsedWod": map[string]interface{}{
			"blocks": []map[string]interface{}{
				{"type": "strength", "name": "3RM Front Squat", "allocatedMinutes": 15},
				{"type": "metcon", "name": "21-15-9 Thrusters & Pull-Ups", "allocatedMinutes": 12},
			},
			"movements":     []string{"Front Squat", "Thruster", "Pull-Up"},
			"primaryJoints": []string{"shoulders", "thoracic_spine", "hips", "knees", "wrists"},
			"stimulus":      "Heavy CNS lower-body loading followed by a high-heart-rate glycolytic sprint taxing front rack & grip.",
		},
		"warmup": map[string]interface{}{
			"style":           "Gamified PVC Broomstick Flow",
			"durationMinutes": 12,
			"phases": []map[string]interface{}{
				{
					"name":            "Phase 1: PVC Circle Drop & Callout (Reaction Game)",
					"durationMinutes": 4,
					"instructions":    "Athletes form a tight circle holding a broomstick/PVC vertically on the floor. Coach calls 'LEFT', 'RIGHT', or 'SPIN'. Athletes must leave their stick and catch the neighbor's stick before it hits the rubber floor. Drop = 3 synchronized air squats with a jump!",
					"coachingGoal":    "Break the ice, spike alertness, get laughter on the floor, and warm up hip/ankle flexion dynamically.",
				},
				{
					"name":            "Phase 2: Broomstick Thoracic, Wrist & Squat Therapy Flow",
					"durationMinutes": 8,
					"instructions":    "Continuous guided movement flow using the broomstick before touching an empty barbell.",
					"coachingGoal":    "Open external shoulder rotation, thoracic extension, and bottom-of-squat posture for Front Squats & Thrusters.",
					"drills": []map[string]interface{}{
						{"name": "PVC Pass-Throughs + Around-the-Worlds", "reps": "10 + 5 each way", "focus": "Pec minor & shoulder capsule opening"},
						{"name": "Broomstick Front Rack Stretch (Elbows High)", "reps": "30 sec / side", "focus": "Triceps & wrist extension for crisp front rack"},
						{"name": "PVC Kang Squats (Good Morning into Deep Squat)", "reps": "8 slow reps", "focus": "Hamstring tension + upright chest in the hole"},
						{"name": "Broomstick Sotts Press + Tempo Thruster", "reps": "6 + 6 reps", "focus": "Pressing mechanics from the bottom of the squat"},
						{"name": "Rig Scapular Pulls + Kip Swings", "reps": "10 + 10 reps", "focus": "Lat engagement and shoulder safety on the rig"},
					},
				},
			},
		},
		"exercises": []map[string]interface{}{
			{
				"name":     "Front Squat",
				"category": "Strength",
				"cues": map[string]interface{}{
					"external": []string{
						"Point your elbow lasers straight at the front wall",
						"Spread the gym floor apart with your shoes on the way up",
						"Drive your shoulders into the bar first out of the bottom",
					},
					"internal": []string{
						"Big 360-degree belly breath at the top and lock your ribs down",
						"Squeeze your upper back tight so the bar sits on a shelf of muscle",
					},
					"commonFaults": []map[string]interface{}{
						{"fault": "Elbows dropping in the bottom of the hole", "fix": "Cue: 'Punch your elbows up to the ceiling before your hips rise'"},
						{"fault": "Heels lifting / knees shooting forward early", "fix": "Cue: 'Sit your hips straight down between your heels and grip the floor with your whole foot'"},
					},
				},
				"scaling": map[string]interface{}{
					"rxPlus":         "3-second pause at the bottom of the 1st rep of every set (3RM with pause)",
					"scaled":         "5x4 Front Squat at moderate load focusing on perfect elbow position, or Heavy Goblet Squat",
					"seniorsMasters": "Front Squat to a 14-16\" Box (consistent depth) or Dual Kettlebell/Dumbbell Front Squat (easier on wrists/shoulders)",
					"injured": map[string]interface{}{
						"wristOrShoulder": "Cross-arm Zombie Front Squat, Strap-Grip Front Squat (holding lifting straps), or Back Squat",
						"kneePain":        "Slow 3-1-1 Tempo Box Squat above parallel with vertical shins, or Heavy Romanian Deadlift (RDL)",
						"lowerBack":       "Belt Squat / Bulgarian Split Squat with dumbbells (unloads axial spinal compression)",
					},
				},
			},
			{
				"name":     "Thruster",
				"category": "Metcon",
				"cues": map[string]interface{}{
					"external": []string{
						"Launch the bar off your shoulders like a cannon using your hip pop",
						"Get your ears in front of your biceps at the top lockout",
						"Wait until the bar touches your shoulders before starting the next squat",
					},
					"internal": []string{
						"Exhale sharply at the top of the press; inhale on the descent",
						"Keep your core braced as the bar crashes back to the rack",
					},
					"commonFaults": []map[string]interface{}{
						{"fault": "Pressing with the arms too early while still squatting up", "fix": "Cue: 'Violent hip snap first—let your legs make the bar weightless'"},
						{"fault": "Pulling the bar down into the next squat too early (losing balance forward)", "fix": "Cue: 'Rack first, then squat'"},
					},
				},
				"scaling": map[string]interface{}{
					"rxPlus":         "115/80 lb Barbell Thrusters unbroken sets target",
					"scaled":         "65/45 lb Barbell or Dual Dumbbell Thrusters (35/20 lb)",
					"seniorsMasters": "Dual Dumbbell Squat-to-Target + Push Press (20/12 lb) or Wall Ball Shots (14/10 lb)",
					"injured": map[string]interface{}{
						"wristOrShoulder": "Single-Arm Dumbbell Thruster (healthy side) or Goblet Squat + Russian Kettlebell Swing combo",
						"kneePain":        "Push Press from the rack (75/55 lb) — keeps the upper-body & metabolic stimulus without deep knee flexion",
						"lowerBack":       "Light Dual DB Front Squat to Box + Strict Press (controlled tempo)",
					},
				},
			},
			{
				"name":     "Pull-Up",
				"category": "Metcon",
				"cues": map[string]interface{}{
					"external": []string{
						"Push the bar away from you at the top to cycle directly into the next kip",
						"Snap your toes forward and pull your chin over the horizontal plane",
						"Keep your feet glued together like you're wearing a mermaid fin",
					},
					"internal": []string{
						"Depress your shoulder blades (active hang) before initiating the swing",
						"Hollow body to arch—initiate from the lats and core, not the knees",
					},
					"commonFaults": []map[string]interface{}{
						{"fault": "Dropping straight down from the top and losing the kip rhythm", "fix": "Cue: 'Push back away from the bar at the top like a pendulum'"},
						{"fault": "Bending knees wildly in the backswing", "fix": "Cue: 'Squeeze your quads and glutes tight—press your chest through the window'"},
					},
				},
				"scaling": map[string]interface{}{
					"rxPlus":         "Chest-to-Bar Pull-Ups (21-15-9) or 12-9-6 Bar Muscle-Ups",
					"scaled":         "Jumping Pull-Ups (controlled descent) or Banded Kipping/Strict Pull-Ups",
					"seniorsMasters": "Ring Rows (feet under rings, strict 2-sec lower) or Seated Banded Lat Pulldowns",
					"injured": map[string]interface{}{
						"wristOrShoulder": "Strict Ring Rows (neutral grip) or Bent-Over Dual Dumbbell Rows",
						"kneePain":        "Strict Pull-Ups or Banded Strict Pull-Ups (no jumping impact on landing)",
						"lowerBack":       "Supported Chest-Supported Incline DB Row or Strict Ring Row with glutes braced",
					},
				},
			},
		},
		"timeline": map[string]interface{}{
			"totalMinutes":      60,
			"feasibilityStatus": "OPTIMAL",
			"coachWarnings": []string{
				"Have athletes load their Strength Front Squat barbell on the rig and keep their Metcon plates nearby for a fast 4-minute strip-down transition.",
				"Cap the 21-15-9 Metcon strictly at 12:00 so everyone gets a full 6-minute cooldown and floor reset.",
			},
			"blocks": []map[string]interface{}{
				{"startMin": 0, "endMin": 4, "label": "Whiteboard Brief, Stimulus & Injury Check", "type": "brief", "notes": "Highlight 3RM Front Squat goal & fast 21-15-9 sprint intent; ask privately about wrists/shoulders."},
				{"startMin": 4, "endMin": 8, "label": "Warm-Up Phase 1: PVC Circle Drop Reaction Game", "type": "warmup", "notes": "High energy! Call LEFT/RIGHT/SPIN."},
				{"startMin": 8, "endMin": 16, "label": "Warm-Up Phase 2: Broomstick Thoracic & Squat Therapy", "type": "warmup", "notes": "Check front-rack elbow height and squat depth."},
				{"startMin": 16, "endMin": 20, "label": "Transition: Rack Assignment & Empty Bar Build-Up", "type": "transition", "notes": "Pair athletes 2 per rack by similar height."},
				{"startMin": 20, "endMin": 35, "label": "Strength: 15-Min to Find 3RM Front Squat (Every 2:30 x 6 Sets)", "type": "strength", "notes": "Sets at 0:00, 2:30, 5:00, 7:30, 10:00, 12:30. Walk the line cueing 'Elbows Up!'"},
				{"startMin": 35, "endMin": 42, "label": "Transition: Strip Bars to Metcon Weight & Rig Scaling Check", "type": "transition", "notes": "Run a quick 4-rep Thruster + 4-rep Pull-Up rehearsal round."},
				{"startMin": 42, "endMin": 54, "label": "Engine / Metcon: 21-15-9 Thrusters & Pull-Ups (12m Cap)", "type": "metcon", "notes": "Watch lumbar posture on set of 15 Thrusters."},
				{"startMin": 54, "endMin": 60, "label": "Fist Bumps, Chalk Cleanup & Lat/Thoracic Foam Roll", "type": "cooldown", "notes": "60s Puppy Pose on box + 60s Couch Stretch per side."},
			},
		},
		"agentTraces": []map[string]interface{}{
			{"agent": "WodParserAgent", "provider": "gemini", "model": "gemini/gemini-3.8-flash", "durationMs": 420, "status": "completed"},
			{"agent": "WarmupPlannerAgent", "provider": "gemini", "model": "gemini/gemini-3.8-flash", "mcpTool": "query_warmup_drills", "durationMs": 610, "status": "completed"},
			{"agent": "ScalingAndCuesAgent", "provider": "gemini", "model": "gemini/gemini-3.8-flash", "mcpTool": "get_movement_substitutions", "durationMs": 640, "status": "completed"},
			{"agent": "TimelineLogisticsAgent", "provider": "gemini", "model": "gemini/gemini-3.8-flash", "mcpTool": "validate_class_timeline", "durationMs": 310, "status": "completed"},
		},
	}
}

func (ds *DataStore) ensureDemoCoachInPostgres(ctx context.Context) {
	if ds.pgPool == nil {
		return
	}
	demo := ds.coaches[DefaultDemoCoachID]
	_, _ = ds.pgPool.Exec(ctx, `
		INSERT INTO coaches (id, email, password_hash, display_name, box_name, default_class_duration_min)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO NOTHING
	`, demo.ID, demo.Email, demo.PasswordHash, demo.DisplayName, demo.BoxName, demo.DefaultClassDurationMin)
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
	return ds.coaches[DefaultDemoCoachID], nil
}

func (ds *DataStore) UpdateCoachProfile(ctx context.Context, coachID, displayName, boxName string, defaultDuration int) (*models.Coach, error) {
	if defaultDuration <= 0 {
		defaultDuration = 60
	}
	if ds.pgPool != nil {
		_, _ = ds.pgPool.Exec(ctx, `
			UPDATE coaches SET display_name = $2, box_name = $3, default_class_duration_min = $4, updated_at = NOW()
			WHERE id = $1
		`, coachID, displayName, boxName, defaultDuration)
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	c, ok := ds.coaches[coachID]
	if !ok {
		c = ds.coaches[DefaultDemoCoachID]
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
		roleMap = ds.agentPrefs[DefaultDemoCoachID]
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
