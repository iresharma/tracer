package collector

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// basicAuthMiddleware requires HTTP Basic Auth matching user/pass before
// calling next. If both user and pass are empty, auth is disabled and
// requests pass through untouched — this is the documented default so
// deploys without the auth Secret configured don't break at startup.
func basicAuthMiddleware(next http.Handler, user, pass string) http.Handler {
	if user == "" && pass == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqUser, reqPass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(reqUser), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(reqPass), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="tracer-collector"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerAuthMiddleware requires an `Authorization: Bearer <token>` header
// matching token before calling next. If token is empty, auth is disabled
// and requests pass through untouched.
func bearerAuthMiddleware(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		reqToken, ok := strings.CutPrefix(auth, prefix)
		if !ok || subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
