package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the payload embedded in every JWT issued by this API.
type Claims struct {
	Login string `json:"login"`
	jwt.RegisteredClaims
}

// Service handles token generation and validation.
type Service struct {
	secret      []byte
	expiresIn   time.Duration
}

// NewService creates an auth Service using the given secret and expiration window.
func NewService(secret string, expiresHours int) *Service {
	return &Service{
		secret:    []byte(secret),
		expiresIn: time.Duration(expiresHours) * time.Hour,
	}
}

// GenerateToken creates and signs a JWT for the given login.
func (s *Service) GenerateToken(login string) (string, error) {
	now := time.Now()
	claims := Claims{
		Login: login,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiresIn)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
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
