package middleware

import "net/http"

// SecurityHeaders adds defensive HTTP headers to every response.
// These help protect against common browser-based attacks even though
// CSRF is not a concern for a Bearer-token API.
//
// Applied globally by wrapping the entire ServeMux in main.go.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME-type sniffing attacks.
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Disallow embedding in iframes.
		w.Header().Set("X-Frame-Options", "DENY")

		// Enforce HTTPS for 2 years (only effective when served over TLS).
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")

		// Disable caching for all API responses.
		w.Header().Set("Cache-Control", "no-store")

		next.ServeHTTP(w, r)
	})
}
