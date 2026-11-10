package store

import (
	"context"
	"testing"
	"time"

	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/models"
)

func newTestStore(t *testing.T) *DataStore {
	vault, err := crypto.NewVault("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}
	ctx := context.Background()
	return NewDataStore(ctx, vault, "", "", "", "")
}

func TestBuildEphemeralRoutingConfig_Success(t *testing.T) {
	ds := newTestStore(t)
	ctx := context.Background()
	coachID := "coach-test-111"

	// 1. Save Primary Gemini Key
	geminiKey := "AIzaSyD-TestGeminiKey12345"
	_, err := ds.SaveAPIKey(ctx, coachID, "gemini", geminiKey, true)
	if err != nil {
		t.Fatalf("SaveAPIKey failed: %v", err)
	}

	// 2. Save Secondary OpenAI Key
	openAIKey := "sk-proj-TestOpenAIKey99999"
	_, err = ds.SaveAPIKey(ctx, coachID, "openai", openAIKey, false)
	if err != nil {
		t.Fatalf("SaveAPIKey openai failed: %v", err)
	}

	// 3. Configure Warmup Agent to use OpenAI
	_, err = ds.SaveAgentPrefs(ctx, coachID, []models.AgentModelPref{
		{
			AgentRole:       "warmup",
			Provider:        "openai",
			ModelIdentifier: "openai/gpt-4o",
		},
	})
	if err != nil {
		t.Fatalf("SaveAgentPrefs failed: %v", err)
	}

	// 4. Build routing config
	routes, err := ds.BuildEphemeralRoutingConfig(ctx, coachID)
	if err != nil {
		t.Fatalf("BuildEphemeralRoutingConfig returned error: %v", err)
	}

	// Parser should default to Primary (Gemini)
	if routes["parser"].Provider != "gemini" || routes["parser"].APIKey != geminiKey {
		t.Errorf("expected parser to use gemini primary key, got %+v", routes["parser"])
	}

	// Warmup should use OpenAI override
	if routes["warmup"].Provider != "openai" || routes["warmup"].APIKey != openAIKey {
		t.Errorf("expected warmup to use openai override key, got %+v", routes["warmup"])
	}
}

func TestBuildEphemeralRoutingConfig_NoKeysError(t *testing.T) {
	ds := newTestStore(t)
	ctx := context.Background()
	coachID := "coach-empty-no-keys"

	_, err := ds.BuildEphemeralRoutingConfig(ctx, coachID)
	if err == nil {
		t.Fatal("expected error when no API keys are configured, got nil")
	}
}

func TestListSessions_CoachIsolationAndImageStripping(t *testing.T) {
	ds := newTestStore(t)
	ctx := context.Background()

	coachA := "coach-alpha"
	coachB := "coach-beta"

	// Save session for Coach A with large imageBase64
	_ = ds.SaveSession(ctx, "session-a1", map[string]interface{}{
		"coachId":   coachA,
		"createdAt": time.Now().Format(time.RFC3339),
		"title":     "Alpha WOD",
		"input": map[string]interface{}{
			"rawText":     "5 Rounds for time",
			"imageBase64": "data:image/jpeg;base64,HUGE_IMAGE_STRING_THAT_SHOULD_BE_STRIPPED",
		},
	})

	// Save session for Coach B
	_ = ds.SaveSession(ctx, "session-b1", map[string]interface{}{
		"coachId":   coachB,
		"createdAt": time.Now().Format(time.RFC3339),
		"title":     "Beta WOD",
		"input": map[string]interface{}{
			"rawText": "AMRAP 20",
		},
	})

	// List sessions for Coach A
	listA, err := ds.ListSessions(ctx, coachA)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("expected 1 session for Coach A, got %d", len(listA))
	}
	if listA[0]["title"] != "Alpha WOD" {
		t.Errorf("expected Alpha WOD, got %v", listA[0]["title"])
	}

	// Verify imageBase64 is stripped in list view
	inputMap, ok := listA[0]["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input map")
	}
	if _, exists := inputMap["imageBase64"]; exists {
		t.Errorf("expected imageBase64 to be stripped from session list view")
	}
}

func TestSharedLink_ExpiryCheck(t *testing.T) {
	ds := newTestStore(t)
	ctx := context.Background()

	pastTime := time.Now().Add(-1 * time.Hour)
	expiredLink := &models.SharedLink{
		ShortCode:         "expired-123",
		MongoSessionID:    "session-xyz",
		CoachID:           "coach-1",
		IncludeCoachNotes: false,
		ExpiresAt:         &pastTime,
		ViewCount:         0,
		CreatedAt:         time.Now().Add(-2 * time.Hour),
	}

	_ = ds.SaveSharedLink(ctx, expiredLink)

	_, err := ds.GetSharedLink(ctx, "expired-123")
	if err == nil {
		t.Fatal("expected expired link to return error")
	}
}
