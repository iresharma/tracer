package collector

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// basicAuthMiddleware protects next with HTTP Basic Auth. If both username
// and password are empty, auth is considered disabled and next is returned
// unwrapped. Comparisons use subtle.ConstantTimeCompare to avoid leaking
// timing information about the configured credentials.
func basicAuthMiddleware(username, password string, next http.Handler) http.Handler {
	if username == "" && password == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqUser, reqPass, ok := r.BasicAuth()
		if !ok || !constantTimeEqual(reqUser, username) || !constantTimeEqual(reqPass, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="tracer-collector"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerAuthMiddleware protects next with a static bearer token, for
// service-to-service traffic (the agent's ingest requests). If token is
// empty, auth is considered disabled and next is returned unwrapped.
func bearerAuthMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, prefix) || !constantTimeEqual(strings.TrimPrefix(got, prefix), token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// constantTimeEqual compares two strings in constant time.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
