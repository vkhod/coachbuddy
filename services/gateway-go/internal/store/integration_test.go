package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/google/uuid"
)

// Integration tests run against real databases. They are skipped unless all
// three of these are set (CI sets them with service containers):
//
//	GATEWAY_TEST_POSTGRES_DSN, GATEWAY_TEST_MONGO_URI, GATEWAY_TEST_REDIS_URL
//
// Each test gets an empty Postgres schema (from infra/postgres/init.sql) and
// its own MongoDB database.
func openTestStore(t *testing.T) *DataStore {
	t.Helper()
	pg, mg, rd := os.Getenv("GATEWAY_TEST_POSTGRES_DSN"), os.Getenv("GATEWAY_TEST_MONGO_URI"), os.Getenv("GATEWAY_TEST_REDIS_URL")
	if pg == "" || mg == "" || rd == "" {
		t.Skip("set GATEWAY_TEST_POSTGRES_DSN, GATEWAY_TEST_MONGO_URI and GATEWAY_TEST_REDIS_URL to run integration tests")
	}
	ctx := context.Background()
	dbName := "coachbuddy_test_" + uuid.NewString()[:8]
	ds, err := Open(ctx, testVault(t), Config{PostgresDSN: pg, MongoURI: mg, MongoDB: dbName, RedisURL: rd, Models: modelcatalog.Embedded()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	initSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "infra", "postgres", "init.sql"))
	if err != nil {
		t.Fatalf("read init.sql: %v", err)
	}
	if _, err := ds.pg.Exec(ctx, `DROP TABLE IF EXISTS shared_links, agent_model_prefs, llm_api_keys, coaches CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pg.Exec(ctx, string(initSQL)); err != nil {
		t.Fatalf("apply init.sql: %v", err)
	}
	t.Cleanup(func() {
		_ = ds.db.Drop(context.Background())
		ds.Close()
	})
	return ds
}

func newCoach(t *testing.T, ds *DataStore, email string) *models.Coach {
	t.Helper()
	c, err := ds.CreateCoach(context.Background(), email, "pw-123456", "Coach", "", 0)
	if err != nil {
		t.Fatalf("CreateCoach: %v", err)
	}
	return c
}

func TestOpen_UnreachableDatabaseIsAnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := Open(ctx, testVault(t), Config{
		PostgresDSN: "postgres://x:y@127.0.0.1:1/db?sslmode=disable&connect_timeout=1",
		MongoURI:    "mongodb://127.0.0.1:1",
		RedisURL:    "redis://127.0.0.1:1/0",
		Models:      modelcatalog.Embedded(),
	})
	if err == nil {
		t.Fatal("Open must fail when the databases cannot be reached")
	}
	if _, err := Open(ctx, testVault(t), Config{}); err == nil {
		t.Fatal("Open must fail without connection settings")
	}
}

func TestIntegration_Coaches(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()
	c := newCoach(t, ds, "Alpha@Box.com")

	if _, err := ds.CreateCoach(ctx, "alpha@box.com", "other", "X", "", 0); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate email: want ErrEmailTaken, got %v", err)
	}
	if _, err := ds.CreateCoach(ctx, "", "pw", "X", "", 0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty email: want ErrInvalidInput, got %v", err)
	}
	if got, err := ds.AuthenticateCoach(ctx, " ALPHA@box.com ", "pw-123456"); err != nil || got.ID != c.ID {
		t.Errorf("login: got %v, %v", got, err)
	}
	if _, err := ds.AuthenticateCoach(ctx, "alpha@box.com", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
	if _, err := ds.AuthenticateCoach(ctx, "nobody@box.com", "pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown email: want ErrInvalidCredentials, got %v", err)
	}
	if _, err := ds.GetCoach(ctx, uuid.NewString()); !errors.Is(err, ErrCoachNotFound) {
		t.Errorf("unknown id: want ErrCoachNotFound, got %v", err)
	}
	if _, err := ds.GetCoach(ctx, "not-a-uuid"); !errors.Is(err, ErrCoachNotFound) {
		t.Errorf("malformed id: want ErrCoachNotFound, got %v", err)
	}
	up, err := ds.UpdateCoachProfile(ctx, c.ID, "Vitaly", "CF Bavli", 75)
	if err != nil || up.DisplayName != "Vitaly" || up.BoxName != "CF Bavli" || up.DefaultClassDurationMin != 75 {
		t.Errorf("update: got %+v, %v", up, err)
	}
}

func TestIntegration_KeysPrefsAndRouting(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()
	a := newCoach(t, ds, "a@box.com")
	b := newCoach(t, ds, "b@box.com")

	prefs, err := ds.GetAgentPrefs(ctx, a.ID)
	if err != nil || len(prefs) != len(AgentRoles) || prefs[0].ModelIdentifier != modelcatalog.Embedded().Default().ID {
		t.Fatalf("defaults: got %d prefs, %v", len(prefs), err)
	}

	g, err := ds.SaveAPIKey(ctx, a.ID, "gemini", "AIza-first", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.SaveAPIKey(ctx, a.ID, "openai", "sk-openai", true); err != nil {
		t.Fatal(err)
	}
	// Saving the same provider again replaces the key and keeps one row.
	g2, err := ds.SaveAPIKey(ctx, a.ID, "gemini", "AIza-second", false)
	if err != nil || g2.ID != g.ID {
		t.Fatalf("upsert should keep the row id: %v, %v vs %v", err, g2.ID, g.ID)
	}
	keys, err := ds.ListAPIKeys(ctx, a.ID)
	if err != nil || len(keys) != 2 {
		t.Fatalf("list: %d keys, %v", len(keys), err)
	}
	primaries := 0
	for _, k := range keys {
		if k.IsPrimary {
			primaries++
			if k.Provider != "openai" {
				t.Errorf("primary should be openai, got %s", k.Provider)
			}
		}
	}
	if primaries != 1 {
		t.Errorf("expected exactly one primary key, got %d", primaries)
	}

	if _, err := ds.SaveAgentPrefs(ctx, a.ID, []models.AgentModelPref{{AgentRole: "bogus"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown role: want ErrInvalidInput, got %v", err)
	}
	if _, err := ds.SaveAgentPrefs(ctx, a.ID, []models.AgentModelPref{{AgentRole: "warmup", Provider: "gemini", ModelIdentifier: "gemini/x"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("model not in the list: want ErrInvalidInput, got %v", err)
	}
	if _, err := ds.SaveAPIKey(ctx, a.ID, "groq", "gsk-x", false); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("provider not in the list: want ErrInvalidInput, got %v", err)
	}
	saved, err := ds.SaveAgentPrefs(ctx, a.ID, []models.AgentModelPref{{AgentRole: "warmup", Provider: "gemini", ModelIdentifier: "gemini/gemini-3.5-flash-lite"}})
	if err != nil || len(saved) != 1 || saved[0].AgentRole != "warmup" {
		t.Fatalf("save prefs: %+v, %v", saved, err)
	}

	routes, err := ds.BuildEphemeralRoutingConfig(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := routes["warmup"]; r.APIKey != "AIza-second" || r.Model != "gemini/gemini-3.5-flash-lite" {
		t.Errorf("warmup route: %+v", r)
	}
	if r := routes["parser"]; r.Provider != "openai" || r.APIKey != "sk-openai" || r.Model != "openai/gpt-6-luna" {
		t.Errorf("parser should use the openai primary: %+v", r)
	}

	// Another coach cannot delete a's key, and has no keys of their own.
	if err := ds.DeleteAPIKey(ctx, b.ID, g.ID); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("delete by other coach: want ErrKeyNotFound, got %v", err)
	}
	if _, err := ds.BuildEphemeralRoutingConfig(ctx, b.ID); err == nil {
		t.Error("routing for a coach without keys must fail")
	}
	if err := ds.DeleteAPIKey(ctx, a.ID, g.ID); err != nil {
		t.Errorf("delete by owner: %v", err)
	}
	if err := ds.DeleteAPIKey(ctx, a.ID, "not-a-uuid"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("malformed key id: want ErrKeyNotFound, got %v", err)
	}
}

func TestIntegration_Sessions(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()

	for i, s := range []struct{ id, coach string }{{"s-old", "alpha"}, {"s-new", "alpha"}, {"s-beta", "beta"}} {
		doc := map[string]interface{}{
			"coachId":   s.coach,
			"createdAt": time.Date(2026, 11, 18, 10, i, 0, 0, time.UTC).Format(time.RFC3339),
			"input":     map[string]interface{}{"rawText": "6RFT", "imageBase64": "data:image/png;base64,HUGE"},
			"timeline":  map[string]interface{}{"blocks": []interface{}{map[string]interface{}{"label": "Warm-up", "notes": "n"}}},
		}
		if err := ds.SaveSession(ctx, s.id, doc); err != nil {
			t.Fatal(err)
		}
	}

	doc, err := ds.GetSession(ctx, "s-new")
	if err != nil {
		t.Fatal(err)
	}
	// Nested documents must come back as maps and slices, the shapes the
	// handlers and the share sanitiser work with.
	tl, ok := doc["timeline"].(map[string]interface{})
	if !ok {
		t.Fatalf("timeline decoded as %T, want map", doc["timeline"])
	}
	blocks, ok := tl["blocks"].([]interface{})
	if !ok {
		t.Fatalf("blocks decoded as %T, want []interface{}", tl["blocks"])
	}
	if _, ok := blocks[0].(map[string]interface{}); !ok {
		t.Fatalf("block decoded as %T, want map", blocks[0])
	}
	if _, err := ds.GetSession(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("missing: want ErrSessionNotFound, got %v", err)
	}

	list, err := ds.ListSessions(ctx, "alpha")
	if err != nil || len(list) != 2 {
		t.Fatalf("list alpha: %d, %v", len(list), err)
	}
	if list[0]["sessionId"] != "s-new" {
		t.Errorf("newest first: got %v", list[0]["sessionId"])
	}
	in, ok := list[0]["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("list input decoded as %T, want map", list[0]["input"])
	}
	if _, has := in["imageBase64"]; has || in["rawText"] != "6RFT" {
		t.Errorf("list must drop the screenshot and keep the rest: %v", in)
	}
	empty, err := ds.ListSessions(ctx, "nobody")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("no sessions: want empty slice, got %v, %v", empty, err)
	}

	if err := ds.DeleteSession(ctx, "s-old"); err != nil {
		t.Fatal(err)
	}
	if err := ds.DeleteSession(ctx, "s-old"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("second delete: want ErrSessionNotFound, got %v", err)
	}
}

func TestIntegration_SharedLinks(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()
	c := newCoach(t, ds, "share@box.com")

	link := &models.SharedLink{ShortCode: "cb-aaa", MongoSessionID: "s1", CoachID: c.ID, QRSVG: "<svg/>"}
	if err := ds.SaveSharedLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 2; want++ {
		got, err := ds.GetSharedLink(ctx, "cb-aaa")
		if err != nil || got.ViewCount != want || got.QRSVG != "<svg/>" {
			t.Fatalf("view %d: %+v, %v", want, got, err)
		}
	}

	past := time.Now().Add(-time.Hour)
	if err := ds.SaveSharedLink(ctx, &models.SharedLink{ShortCode: "cb-old", MongoSessionID: "s1", CoachID: c.ID, ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.GetSharedLink(ctx, "cb-old"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("expired: want ErrLinkNotFound, got %v", err)
	}
	if _, err := ds.GetSharedLink(ctx, "cb-none"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("unknown: want ErrLinkNotFound, got %v", err)
	}
}

func TestIntegration_SessionEvents(t *testing.T) {
	ds := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := "evt-" + uuid.NewString()[:8]
	t.Cleanup(func() { ds.redis.Del(context.Background(), eventsBuffer+id) })

	first := `{"step":"parser"}`
	ds.redis.RPush(ctx, eventsBuffer+id, first)

	stream, err := ds.SessionEvents(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(stream.Buffered) != 1 || stream.Buffered[0] != first {
		t.Fatalf("buffered: %v", stream.Buffered)
	}

	// A duplicate of a buffered event is dropped; a new one arrives live.
	ds.redis.Publish(ctx, eventsChannel+id, first)
	done := `{"step":"completed"}`
	ds.redis.Publish(ctx, eventsChannel+id, done)
	select {
	case got := <-stream.Live:
		if got != done {
			t.Errorf("live: got %s, want %s", got, done)
		}
	case <-ctx.Done():
		t.Fatal("no live event")
	}

	if errs := ds.Ping(ctx); errs["postgres"] != nil || errs["mongodb"] != nil || errs["redis"] != nil {
		t.Errorf("ping: %v", errs)
	}
}
