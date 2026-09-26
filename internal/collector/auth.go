package collector

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// requireBasicAuth gates next behind HTTP Basic Auth. If both username and
// password are empty, auth is disabled and next is returned unchanged —
// this keeps local dev and existing deployments working without any
// configuration. Credential comparison hashes both sides first so
// subtle.ConstantTimeCompare always compares equal-length inputs (it's
// only constant-time when lengths match; hashing first avoids leaking
// length via timing and sidesteps that footgun entirely).
func requireBasicAuth(username, password string, next http.Handler) http.Handler {
	if username == "" && password == "" {
		return next
	}
	wantUser := sha256.Sum256([]byte(username))
	wantPass := sha256.Sum256([]byte(password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok := r.BasicAuth()
		gotUserHash := sha256.Sum256([]byte(gotUser))
		gotPassHash := sha256.Sum256([]byte(gotPass))
		userMatch := subtle.ConstantTimeCompare(wantUser[:], gotUserHash[:]) == 1
		passMatch := subtle.ConstantTimeCompare(wantPass[:], gotPassHash[:]) == 1
		if !ok || !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="tracer-collector"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireBearerToken gates next behind a static bearer token supplied via
// the Authorization header. If token is empty, auth is disabled and next
// is returned unchanged.
func requireBearerToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	wantToken := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		got := ""
		if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
			got = auth[len(prefix):]
		}
		gotHash := sha256.Sum256([]byte(got))
		if got == "" || subtle.ConstantTimeCompare(wantToken[:], gotHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tracer-collector"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
