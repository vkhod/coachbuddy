package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const CoachIDContextKey contextKey = "coachID"

type Claims struct {
	CoachID string `json:"coachId"`
	Email   string `json:"email"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret []byte
}

// MinSecretLength is the minimum JWT signing secret length in bytes (HS256).
const MinSecretLength = 32

// NewTokenManager returns an error when the secret is missing or too short.
// There is deliberately no default secret.
func NewTokenManager(secret string) (*TokenManager, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("JWT secret must be at least %d characters, got %d", MinSecretLength, len(secret))
	}
	return &TokenManager{secret: []byte(secret)}, nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (tm *TokenManager) GenerateToken(coachID, email string) (string, error) {
	claims := &Claims{
		CoachID: coachID,
		Email:   email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "coachbuddy-gateway-go",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tm.secret)
}

func (tm *TokenManager) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return tm.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token claims")
}

// Middleware authenticates requests via a Bearer token (or the `token` query parameter for SSE).
// Requests without a valid token are rejected with 401. There is no demo-coach fallback.
func (tm *TokenManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			tokenStr := ""
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			} else if qToken := r.URL.Query().Get("token"); qToken != "" {
				tokenStr = qToken
			}

			if tokenStr == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authorization token required"}`))
				return
			}
			claims, err := tm.ValidateToken(tokenStr)
			if err != nil || claims.CoachID == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid or expired authorization token"}`))
				return
			}

			ctx := context.WithValue(r.Context(), CoachIDContextKey, claims.CoachID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetCoachID returns the authenticated coach ID, or "" when the request did not pass Middleware.
func GetCoachID(ctx context.Context) string {
	if v, ok := ctx.Value(CoachIDContextKey).(string); ok {
		return v
	}
	return ""
}
