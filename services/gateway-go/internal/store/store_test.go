package store

import (
	"testing"

	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
)

const testMasterKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testVault(t *testing.T) *crypto.Vault {
	t.Helper()
	v, err := crypto.NewVault(testMasterKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	return v
}

func encryptedKey(t *testing.T, v *crypto.Vault, coachID, provider, raw string, primary bool) *models.LLMAPIKey {
	t.Helper()
	ct, nonce, err := v.Encrypt(coachID, provider, raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return &models.LLMAPIKey{CoachID: coachID, Provider: provider, EncryptedKey: ct, Nonce: nonce, IsPrimary: primary}
}

var catalog = modelcatalog.Embedded()

func TestBuildRoutes_PrimaryAndOverride(t *testing.T) {
	v := testVault(t)
	coach := "coach-test-111"
	keys := []*models.LLMAPIKey{
		encryptedKey(t, v, coach, "gemini", "AIzaSyD-TestGeminiKey12345", true),
		encryptedKey(t, v, coach, "openai", "sk-proj-TestOpenAIKey99999", false),
	}
	prefs := []*models.AgentModelPref{{AgentRole: "warmup", Provider: "openai", ModelIdentifier: "openai/gpt-5.4-mini"}}

	routes, err := buildRoutes(v, catalog, coach, keys, prefs)
	if err != nil {
		t.Fatal(err)
	}
	if r := routes["parser"]; r.Provider != "gemini" || r.APIKey != "AIzaSyD-TestGeminiKey12345" || r.Model != "gemini/gemini-3.8-flash" {
		t.Errorf("parser should use the primary gemini key and default model, got %+v", r)
	}
	if r := routes["warmup"]; r.Provider != "openai" || r.APIKey != "sk-proj-TestOpenAIKey99999" || r.Model != "openai/gpt-5.4-mini" {
		t.Errorf("warmup should use the openai override, got %+v", r)
	}
	if len(routes) != len(AgentRoles) {
		t.Errorf("expected a route for every role, got %d", len(routes))
	}
}

func TestBuildRoutes_GlobalChoiceAppliesToEveryRole(t *testing.T) {
	v := testVault(t)
	coach := "coach-g"
	keys := []*models.LLMAPIKey{encryptedKey(t, v, coach, "anthropic", "sk-ant-x", true)}
	prefs := []*models.AgentModelPref{{AgentRole: "global", Provider: "anthropic", ModelIdentifier: "anthropic/claude-haiku-5-5"}}
	routes, err := buildRoutes(v, catalog, coach, keys, prefs)
	if err != nil {
		t.Fatal(err)
	}
	if r := routes["timeline"]; r.Model != "anthropic/claude-haiku-5-5" {
		t.Errorf("timeline should use the global choice, got %+v", r)
	}
}

func TestBuildRoutes_ChoiceWithoutKeyOrOffTheListUsesPrimary(t *testing.T) {
	v := testVault(t)
	coach := "coach-1"
	keys := []*models.LLMAPIKey{
		encryptedKey(t, v, coach, "anthropic", "sk-ant-x", true),
		encryptedKey(t, v, coach, "groq", "gsk-old", false),
	}
	prefs := []*models.AgentModelPref{
		{AgentRole: "scaling", Provider: "groq", ModelIdentifier: "groq/llama-3.3-70b-versatile"},
		{AgentRole: "warmup", Provider: "openai", ModelIdentifier: "openai/gpt-6-luna"},
		{AgentRole: "timeline", Provider: "anthropic", ModelIdentifier: "anthropic/claude-3-7-sonnet-latest"},
	}

	routes, err := buildRoutes(v, catalog, coach, keys, prefs)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"scaling", "warmup", "timeline"} {
		if r := routes[role]; r.Provider != "anthropic" || r.Model != "anthropic/claude-sonnet-5-5" || r.APIKey != "sk-ant-x" {
			t.Errorf("%s: expected the anthropic primary and its default model, got %+v", role, r)
		}
	}
}

func TestBuildRoutes_OnlyAKeyForADroppedProviderIsAnError(t *testing.T) {
	v := testVault(t)
	keys := []*models.LLMAPIKey{encryptedKey(t, v, "coach-1", "groq", "gsk-old", true)}
	if _, err := buildRoutes(v, catalog, "coach-1", keys, nil); err == nil {
		t.Fatal("a groq key alone must not route")
	}
}

func TestBuildRoutes_NoUsableKeyIsAnError(t *testing.T) {
	v := testVault(t)
	if _, err := buildRoutes(v, catalog, "coach-1", nil, defaultAgentPrefs(catalog)); err == nil {
		t.Fatal("expected an error with no keys")
	}
	// A key encrypted for another coach does not decrypt, so it does not count.
	other := encryptedKey(t, v, "coach-2", "gemini", "AIza-other", true)
	if _, err := buildRoutes(v, catalog, "coach-1", []*models.LLMAPIKey{other}, nil); err == nil {
		t.Fatal("expected an error when no key decrypts for this coach")
	}
}

func TestOrderPrefs_FollowsAgentRoles(t *testing.T) {
	got := orderPrefs([]*models.AgentModelPref{{AgentRole: "timeline"}, {AgentRole: "global"}, {AgentRole: "warmup"}})
	want := []string{"global", "warmup", "timeline"}
	for i, p := range got {
		if p.AgentRole != want[i] {
			t.Fatalf("order: got %s at %d, want %s", p.AgentRole, i, want[i])
		}
	}
}
