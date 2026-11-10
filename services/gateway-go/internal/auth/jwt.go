package auth

import (
	"context"
	"errors"
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

func NewTokenManager(secret string) *TokenManager {
	if strings.TrimSpace(secret) == "" {
		secret = "coachbuddy-super-secret-jwt-signing-key-2026"
	}
	return &TokenManager{secret: []byte(secret)}
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

// Middleware authenticates requests via Bearer token or falls back to the default demo coach when allowDemo is true.
func (tm *TokenManager) Middleware(defaultCoachID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			tokenStr := ""
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			} else if qToken := r.URL.Query().Get("token"); qToken != "" {
				tokenStr = qToken
			}

			coachID := defaultCoachID
			if tokenStr != "" {
				claims, err := tm.ValidateToken(tokenStr)
				if err != nil || claims.CoachID == "" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"invalid or expired authorization token"}`))
					return
				}
				coachID = claims.CoachID
			} else if defaultCoachID == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authorization token required"}`))
				return
			}

			ctx := context.WithValue(r.Context(), CoachIDContextKey, coachID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetCoachID(ctx context.Context) string {
	if v, ok := ctx.Value(CoachIDContextKey).(string); ok && v != "" {
		return v
	}
	return "coach-demo-001"
}
