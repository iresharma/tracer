package collector

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireBasicAuth returns middleware enforcing HTTP Basic auth against a
// single configured username/password pair. If both are empty, auth is
// considered disabled and the returned middleware is a no-op passthrough —
// New() rejects the case where only one of the two is set, so by the time
// this runs it's either "both set" or "both empty".
func requireBasicAuth(username, password string) func(http.Handler) http.Handler {
	if username == "" && password == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, gotPass, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(gotUser), []byte(username)) != 1 ||
				subtle.ConstantTimeCompare([]byte(gotPass), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="tracer-collector"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireBearerToken returns middleware enforcing a static bearer token via
// the Authorization header. If token is empty, auth is disabled and the
// returned middleware is a no-op passthrough.
func requireBearerToken(token string) func(http.Handler) http.Handler {
	if token == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	const prefix = "bearer "
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="tracer-collector"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			got := auth[len(prefix):]
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="tracer-collector"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
