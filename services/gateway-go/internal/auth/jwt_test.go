package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenManager_GenerateAndValidate(t *testing.T) {
	tm := mustTokenManager(t)
	token, err := tm.GenerateToken("coach-123", "coach@test.com")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := tm.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if claims.CoachID != "coach-123" {
		t.Errorf("expected coachId 'coach-123', got %s", claims.CoachID)
	}
	if claims.Email != "coach@test.com" {
		t.Errorf("expected email 'coach@test.com', got %s", claims.Email)
	}
}

func TestTokenManager_Middleware_RejectsInvalidToken(t *testing.T) {
	tm := mustTokenManager(t)
	handler := tm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer invalid-tampered-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized for bad token, got %d", rr.Code)
	}
}

func TestTokenManager_Middleware_RejectsMissingToken(t *testing.T) {
	tm := mustTokenManager(t)
	called := false
	handler := tm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d", rr.Code)
	}
	if called {
		t.Error("handler must not run without a token")
	}
}

func TestNewTokenManager_RejectsMissingOrShortSecret(t *testing.T) {
	for _, secret := range []string{"", "   ", "too-short-secret"} {
		if _, err := NewTokenManager(secret); err == nil {
			t.Errorf("expected an error for secret %q", secret)
		}
	}
}

func mustTokenManager(t *testing.T) *TokenManager {
	t.Helper()
	tm, err := NewTokenManager("test-secret-key-32-chars-long-12345")
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return tm
}
