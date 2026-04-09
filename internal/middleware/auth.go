package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with other packages.
type contextKey string

const ClaimsKey contextKey = "claims"

// Authenticate is an HTTP middleware that validates the Bearer token present in
// the Authorization header. On success it injects the JWT claims into the
// request context and calls the next handler. On failure it responds with 401.
func Authenticate(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing Authorization header"})
				return
			}

			// Expected format: "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed Authorization header"})
				return
			}

			claims, err := svc.ValidateToken(parts[1])
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired token"})
				return
			}

			// Inject claims into context so downstream handlers can read them.
			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClaims retrieves the JWT claims injected by the Authenticate middleware.
func GetClaims(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(ClaimsKey).(*auth.Claims)
	return claims
}
