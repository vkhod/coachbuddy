package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coachbuddy/gateway-go/internal/contract"
	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
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

func TestIntegration_Plans(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()

	for i, p := range []struct{ id, coach string }{{"pl-old", "alpha"}, {"pl-new", "alpha"}, {"pl-beta", "beta"}} {
		doc := map[string]interface{}{
			"planId":    p.id,
			"coachId":   p.coach,
			"revision":  1,
			"createdAt": time.Date(2026, 11, 18, 10, i, 0, 0, time.UTC).Format(time.RFC3339),
			"input":     map[string]interface{}{"imageRef": "img-" + p.id, "classMinutes": 60},
			"timeline":  map[string]interface{}{"blocks": []interface{}{map[string]interface{}{"label": "Warm-up"}}},
		}
		if err := ds.CreatePlan(ctx, doc); err != nil {
			t.Fatal(err)
		}
		if err := ds.SavePlanImage(ctx, &models.PlanImage{Ref: "img-" + p.id, CoachID: p.coach, MimeType: "image/png", Data: []byte{1, 2, 3}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ds.CreatePlan(ctx, map[string]interface{}{"planId": "pl-new"}); err == nil {
		t.Error("creating a plan twice must fail")
	}

	doc, err := ds.GetPlan(ctx, "pl-new")
	if err != nil {
		t.Fatal(err)
	}
	if _, has := doc["_id"]; has {
		t.Error("the plan must come back without MongoDB's _id")
	}
	// Nested documents must come back as maps and slices, the shapes the
	// handlers and the share views work with.
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
	if _, err := ds.GetPlan(ctx, "missing"); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("missing: want ErrPlanNotFound, got %v", err)
	}

	doc["revision"] = 2
	if err := ds.SavePlan(ctx, "pl-new", doc); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("SavePlan must not move a plan to another revision: %v", err)
	}
	if err := ds.AdvancePlan(ctx, "pl-new", 1, doc); err != nil {
		t.Fatal(err)
	}
	if err := ds.AdvancePlan(ctx, "pl-new", 1, doc); !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("a second change from revision 1: want ErrRevisionConflict, got %v", err)
	}
	if err := ds.AdvancePlan(ctx, "missing", 1, doc); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("advance a missing plan: want ErrPlanNotFound, got %v", err)
	}
	// A plan saved as JSON has a float64 revision; it must still match.
	doc["revision"] = float64(2)
	if err := ds.SavePlan(ctx, "pl-new", doc); err != nil {
		t.Fatalf("save at the current revision: %v", err)
	}
	if got, _ := ds.GetPlan(ctx, "pl-new"); RevisionOf(got) != 2 {
		t.Errorf("saved revision: %v (%T)", got["revision"], got["revision"])
	}
	if err := ds.SavePlan(ctx, "missing", doc); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("saving a plan that does not exist must not create it: %v", err)
	}

	list, err := ds.ListPlans(ctx, "alpha")
	if err != nil || len(list) != 2 {
		t.Fatalf("list alpha: %d, %v", len(list), err)
	}
	if list[0]["planId"] != "pl-new" {
		t.Errorf("newest first: got %v", list[0]["planId"])
	}
	empty, err := ds.ListPlans(ctx, "nobody")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("no plans: want empty slice, got %v, %v", empty, err)
	}

	if err := ds.SaveAgentRuns(ctx, &models.AgentRuns{PlanID: "pl-old", CoachID: "alpha", Revision: 1, Runs: []interface{}{map[string]interface{}{"agent": "WodParserAgent"}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ds.db.Collection(runsCollection).CountDocuments(ctx, bson.M{"planId": "pl-old"}); n != 1 {
		t.Errorf("agent runs stored: %d", n)
	}

	if err := ds.DeletePlan(ctx, "pl-old"); err != nil {
		t.Fatal(err)
	}
	if n, _ := ds.db.Collection(imagesCollection).CountDocuments(ctx, bson.M{"_id": "img-pl-old"}); n != 0 {
		t.Error("deleting a plan must delete its screenshot")
	}
	if n, _ := ds.db.Collection(runsCollection).CountDocuments(ctx, bson.M{"planId": "pl-old"}); n != 0 {
		t.Error("deleting a plan must delete its agent runs")
	}
	if n, _ := ds.db.Collection(imagesCollection).CountDocuments(ctx, bson.M{"_id": "img-pl-new"}); n != 1 {
		t.Error("other screenshots must stay")
	}
	if err := ds.DeletePlan(ctx, "pl-old"); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("second delete: want ErrPlanNotFound, got %v", err)
	}
}

func TestIntegration_PlanRevisions(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()
	if err := ds.CreatePlan(ctx, map[string]interface{}{"planId": "pl-r", "coachId": "alpha", "revision": 1}); err != nil {
		t.Fatal(err)
	}
	section, instruction, from := contract.RevisionSectionWarmup, "Use bands", 1
	plan := map[string]interface{}{"planId": "pl-r", "revision": 1, "warmup": map[string]interface{}{"minutes": 12, "blocks": []interface{}{"a"}}}
	revs := []*models.PlanRevision{
		{PlanID: "pl-r", CoachID: "alpha", Revision: 1, CreatedAt: "2026-11-22T10:00:00.000Z", Change: contract.RevisionChange{Kind: contract.RevisionKindCreate}, Sections: []string{"wod", "warmup"}, State: "done", Plan: plan},
		{PlanID: "pl-r", CoachID: "alpha", Revision: 2, CreatedAt: "2026-11-22T10:05:00.000Z", Change: contract.RevisionChange{Kind: contract.RevisionKindRegenerate, Section: &section, Instruction: &instruction}, Sections: []string{"warmup"}, State: "running"},
		{PlanID: "pl-r", CoachID: "alpha", Revision: 3, CreatedAt: "2026-11-22T10:06:00.000Z", Change: contract.RevisionChange{Kind: contract.RevisionKindRestore, RestoredFrom: &from}, State: "done", Plan: plan},
	}
	for _, r := range []int{2, 0, 1} { // stored out of order, listed in order
		if err := ds.SavePlanRevision(ctx, revs[r]); err != nil {
			t.Fatal(err)
		}
	}
	revs[1].State = "done"
	if err := ds.SavePlanRevision(ctx, revs[1]); err != nil { // upsert, no duplicate
		t.Fatal(err)
	}

	list, err := ds.ListPlanRevisions(ctx, "pl-r")
	if err != nil || len(list) != 3 {
		t.Fatalf("list: %d, %v", len(list), err)
	}
	for i, r := range list {
		if r.Revision != i+1 || r.Plan != nil {
			t.Errorf("entry %d: revision %d, plan %v (the list carries no plans)", i, r.Revision, r.Plan != nil)
		}
	}
	if c := list[1].Change; list[1].State != "done" || c.Kind != contract.RevisionKindRegenerate || *c.Section != "warmup" || *c.Instruction != "Use bands" {
		t.Errorf("revision 2: %+v", list[1])
	}
	if list[2].Change.RestoredFrom == nil || *list[2].Change.RestoredFrom != 1 || list[2].Sections != nil {
		t.Errorf("revision 3: %+v", list[2])
	}

	got, err := ds.GetPlanRevision(ctx, "pl-r", 1)
	if err != nil {
		t.Fatal(err)
	}
	w, ok := got.Plan["warmup"].(map[string]interface{})
	if !ok || got.CreatedAt != "2026-11-22T10:00:00.000Z" || len(got.Sections) != 2 {
		t.Errorf("revision 1: %+v", got)
	}
	if _, ok := w["blocks"].([]interface{}); !ok {
		t.Errorf("nested plan fields must decode as plain maps and slices: %T", w["blocks"])
	}
	if _, err := ds.GetPlanRevision(ctx, "pl-r", 9); !errors.Is(err, ErrRevisionNotFound) {
		t.Errorf("missing revision: %v", err)
	}

	if err := ds.DeletePlan(ctx, "pl-r"); err != nil {
		t.Fatal(err)
	}
	if n, _ := ds.db.Collection(revsCollection).CountDocuments(ctx, bson.M{"planId": "pl-r"}); n != 0 {
		t.Errorf("deleting a plan must delete its revisions: %d left", n)
	}
}

func TestIntegration_SharedLinks(t *testing.T) {
	ds := openTestStore(t)
	ctx := context.Background()
	c := newCoach(t, ds, "share@box.com")

	link := &models.SharedLink{ShortCode: "cb-aaa", PlanID: "s1", CoachID: c.ID, QRSVG: "<svg/>"}
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
	if err := ds.SaveSharedLink(ctx, &models.SharedLink{ShortCode: "cb-old", PlanID: "s1", CoachID: c.ID, ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.GetSharedLink(ctx, "cb-old"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("expired: want ErrLinkNotFound, got %v", err)
	}
	if _, err := ds.GetSharedLink(ctx, "cb-none"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("unknown: want ErrLinkNotFound, got %v", err)
	}
}

func TestIntegration_PlanEvents(t *testing.T) {
	ds := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := "evt-" + uuid.NewString()[:8]
	t.Cleanup(func() { ds.redis.Del(context.Background(), eventsBuffer+id) })

	first := `{"step":"parser"}`
	ds.redis.RPush(ctx, eventsBuffer+id, first)

	stream, err := ds.PlanEvents(ctx, id)
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

	// The gateway's own closing event goes into the replay list and out live.
	end := `{"section":"plan","state":"done"}`
	if err := ds.PublishPlanEvent(ctx, id, []byte(end)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-stream.Live:
		if got != end {
			t.Errorf("live: got %s, want %s", got, end)
		}
	case <-ctx.Done():
		t.Fatal("no live closing event")
	}
	if buf, _ := ds.redis.LRange(ctx, eventsBuffer+id, 0, -1).Result(); len(buf) != 2 || buf[1] != end {
		t.Errorf("replay list: %v", buf)
	}
	if ttl := ds.redis.TTL(ctx, eventsBuffer+id).Val(); ttl <= 0 || ttl > eventsTTL {
		t.Errorf("replay list must expire: ttl %v", ttl)
	}

	if errs := ds.Ping(ctx); errs["postgres"] != nil || errs["mongodb"] != nil || errs["redis"] != nil {
		t.Errorf("ping: %v", errs)
	}
}
