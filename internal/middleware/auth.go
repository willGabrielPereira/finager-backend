package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

// Authenticate is an HTTP middleware that validates the Bearer token in the
// Authorization header, checks it against the revocation blocklist, then
// injects the JWT claims into the request context for downstream handlers.
//
// On failure it responds with 401 and does not call next.
func Authenticate(svc *auth.Service, blocklist *repository.BlocklistRepository) func(http.Handler) http.Handler {
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

			rawToken := parts[1]
			claims, err := svc.ValidateToken(rawToken)
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired token"})
				return
			}

			// Check revocation blocklist — token may have been invalidated by logout.
			tokenHash := svc.HashToken(rawToken)
			blocked, err := blocklist.IsBlocked(r.Context(), tokenHash)
			if err != nil || blocked {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "token has been revoked"})
				return
			}

			// Inject claims into context so downstream handlers can read them.
			next.ServeHTTP(w, r.WithContext(auth.InjectClaims(r.Context(), claims)))
		})
	}
}

// GetClaims retrieves the JWT claims from the request context.
// Delegates to the auth package so handlers can use either package.
func GetClaims(r *http.Request) *auth.Claims {
	return auth.GetClaims(r)
}

// GetUserID returns the authenticated user's ID string from context.
func GetUserID(r *http.Request) string {
	return auth.GetUserID(r)
}

// GetFamilyID returns the authenticated user's family ID string from context.
func GetFamilyID(r *http.Request) string {
	return auth.GetFamilyID(r)
}
