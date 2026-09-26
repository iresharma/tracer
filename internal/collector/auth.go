package collector

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/iresharma/tracer/internal/collector/config"
)

// basicAuthMiddleware protects the browser-facing UI and JSON query API
// with HTTP Basic Auth. It's a no-op passthrough when cfg.AuthEnabled is
// false, so existing deployments aren't broken until an operator opts in.
func basicAuthMiddleware(cfg config.Config, next http.Handler) http.Handler {
	if !cfg.AuthEnabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !secureCompare(user, cfg.AuthUsername) || !secureCompare(pass, cfg.AuthPassword) {
			w.Header().Set("WWW-Authenticate", `Basic realm="tracer-collector"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenAuthMiddleware protects the service-to-service ingest endpoint with
// a Bearer token, distinct from the UI/API's Basic Auth credentials so the
// agent's DaemonSet only ever needs one shared secret value, not a
// username/password pair. No-op passthrough when cfg.AuthEnabled is false.
func tokenAuthMiddleware(cfg config.Config, next http.Handler) http.Handler {
	if !cfg.AuthEnabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !secureCompare(token, cfg.AuthIngestToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secureCompare compares two strings in constant time relative to their
// length, avoiding timing side-channels on credential comparisons.
// subtle.ConstantTimeCompare requires equal-length inputs, hence the
// length check first.
func secureCompare(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
