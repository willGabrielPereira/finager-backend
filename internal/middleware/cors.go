package middleware

import (
	"net/http"
	"strings"
)

// CORS handles Cross-Origin Resource Sharing based on a list of allowed origins.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	// Precompute wildcard for fast path
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				if allowAll {
					allowed = true
				} else {
					for _, o := range allowedOrigins {
						if strings.EqualFold(origin, o) {
							allowed = true
							break
						}
					}
				}

				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")
					w.Header().Set("Access-Control-Expose-Headers", "Authorization")
				}
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
