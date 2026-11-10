package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenManager_GenerateAndValidate(t *testing.T) {
	tm := NewTokenManager("test-secret-key-32-chars-long-12345")
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
	tm := NewTokenManager("test-secret-key-32-chars-long-12345")
	handler := tm.Middleware("demo-fallback")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestTokenManager_Middleware_AllowsDemoWhenNoToken(t *testing.T) {
	tm := NewTokenManager("test-secret-key-32-chars-long-12345")
	var capturedCoachID string
	handler := tm.Middleware("demo-coach-id")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCoachID = GetCoachID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 OK for demo fallback, got %d", rr.Code)
	}
	if capturedCoachID != "demo-coach-id" {
		t.Errorf("expected demo coach ID, got %s", capturedCoachID)
	}
}
