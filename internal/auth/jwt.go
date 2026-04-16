package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// — Context helpers ────────────────────────────────────────────────────────────

type contextKey string

const claimsKey contextKey = "auth_claims"

// InjectClaims returns a new context with the JWT claims stored under the
// package-private key. Called by the Authenticate middleware.
func InjectClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// GetClaims retrieves the JWT claims injected by the Authenticate middleware.
// Returns nil if the request has not been authenticated.
func GetClaims(r *http.Request) *Claims {
	claims, _ := r.Context().Value(claimsKey).(*Claims)
	return claims
}

// GetUserID is a convenience helper that returns the authenticated user's ID.
func GetUserID(r *http.Request) string {
	if c := GetClaims(r); c != nil {
		return c.UserID
	}
	return ""
}

// GetFamilyID is a convenience helper that returns the authenticated user's family ID.
func GetFamilyID(r *http.Request) string {
	if c := GetClaims(r); c != nil {
		return c.FamilyID
	}
	return ""
}

// — Claims ─────────────────────────────────────────────────────────────────────

// Claims is the payload embedded in every JWT issued by this API.
// UserID and FamilyID are embedded to avoid a DB lookup on every request.
type Claims struct {
	UserID   string `json:"user_id"`
	Login    string `json:"login"`
	FamilyID string `json:"family_id"`
	jwt.RegisteredClaims
}

// — Service ────────────────────────────────────────────────────────────────────

// Service handles token generation and validation.
type Service struct {
	secret    []byte
	expiresIn time.Duration
}

// NewService creates an auth Service using the given secret and expiration window.
func NewService(secret string, expiresHours int) *Service {
	return &Service{
		secret:    []byte(secret),
		expiresIn: time.Duration(expiresHours) * time.Hour,
	}
}

// GenerateToken creates and signs a JWT for the given user.
// The token embeds UserID, Login and FamilyID so handlers can read them
// from context without hitting the database.
func (s *Service) GenerateToken(user *models.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   user.ID.Hex(),
		Login:    user.Login,
		FamilyID: user.FamilyID.Hex(),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiresIn)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// GenerateRefreshToken creates a cryptographically random opaque token.
// Returns the raw token (sent to the client) and its SHA-256 hash (stored in DB).
// The raw token is never persisted anywhere — only the hash is.
func (s *Service) GenerateRefreshToken() (rawToken string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	rawToken = base64.URLEncoding.EncodeToString(b)
	hash = s.HashToken(rawToken)
	return
}

// HashToken returns the SHA-256 hex digest of a token string.
// Used to safely store and look up tokens without exposing the raw value.
func (s *Service) HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ValidateToken parses and validates a JWT string, returning its claims.
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
