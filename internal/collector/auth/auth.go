// Package auth provides simple, self-hosted HTTP authentication middleware
// for the collector: a bearer-token check for the agent-to-collector
// ingest path, and a basic-auth check for the browser-facing UI and JSON
// query API. Both are deliberately minimal (no external identity
// provider, no sessions) — appropriate for a homelab deployment where the
// credential is a single shared secret set via config/env, not a login
// system with multiple users.
package auth

import (
	"crypto/subtle"
	"log"
	"net/http"
)

// RequireIngestToken wraps next with a bearer-token check against token,
// intended for the service-to-service agent -> collector ingest path
// (POST /api/v1/logs), not for browser clients. Compares with
// subtle.ConstantTimeCompare rather than ==, since this guards a shared
// secret.
//
// If token is empty, the wrapped handler runs unauthenticated (a warning
// is logged once at construction by the caller) — this keeps existing
// zero-config deployments and tests working, but leaves the route open,
// so operators must set an ingest token before exposing the collector
// beyond a trusted network.
func RequireIngestToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tracer-collector-ingest"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireBasicAuth wraps next with an HTTP Basic auth check against user
// and pass, intended for the browser-facing UI and JSON query API. Uses
// r.BasicAuth() for parsing and subtle.ConstantTimeCompare for the actual
// comparison, since naive == comparisons on secrets are timing-unsafe.
//
// If either user or pass is empty, the wrapped handler runs
// unauthenticated for the same zero-config reason as RequireIngestToken.
func RequireBasicAuth(user, pass string, next http.Handler) http.Handler {
	if user == "" || pass == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok := r.BasicAuth()
		if !ok || !constantTimeEqual(gotUser, user) || !constantTimeEqual(gotPass, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="tracer-collector"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	// Compare lengths first (ConstantTimeCompare requires equal-length
	// inputs and would otherwise always return 0), then the bytes.
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// WarnIfUnset logs a warning naming which of the two auth surfaces are
// running unauthenticated, so an operator notices during startup rather
// than discovering it by exposing the Service. Called once from
// collector.New; kept here so the warning text stays next to the
// middleware it describes.
func WarnIfUnset(ingestToken, uiUser, uiPass string) {
	if ingestToken == "" {
		log.Printf("collector: WARNING: INGEST_AUTH_TOKEN is not set — POST /api/v1/logs is unauthenticated; set it before accepting logs from outside a trusted network")
	}
	if uiUser == "" || uiPass == "" {
		log.Printf("collector: WARNING: UI_AUTH_USERNAME/UI_AUTH_PASSWORD are not set — the UI and JSON query API are unauthenticated; set them before exposing the collector Service beyond a trusted network")
	}
}
