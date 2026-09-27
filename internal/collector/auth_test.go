package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler is a trivial handler that records whether it was reached and
// writes 200 so tests can distinguish "auth middleware let this through"
// from "auth middleware rejected before delegating".
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestBasicAuthMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		cfgUser      string
		cfgPass      string
		reqUser      string
		reqPass      string
		setAuth      bool
		wantStatus   int
		wantReached  bool
		wantWWWAuthn bool
	}{
		{
			name:        "auth disabled when no creds configured",
			cfgUser:     "",
			cfgPass:     "",
			setAuth:     false,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "correct credentials pass",
			cfgUser:     "admin",
			cfgPass:     "secret",
			reqUser:     "admin",
			reqPass:     "secret",
			setAuth:     true,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:         "missing credentials rejected",
			cfgUser:      "admin",
			cfgPass:      "secret",
			setAuth:      false,
			wantStatus:   http.StatusUnauthorized,
			wantReached:  false,
			wantWWWAuthn: true,
		},
		{
			name:         "wrong password rejected",
			cfgUser:      "admin",
			cfgPass:      "secret",
			reqUser:      "admin",
			reqPass:      "wrong",
			setAuth:      true,
			wantStatus:   http.StatusUnauthorized,
			wantReached:  false,
			wantWWWAuthn: true,
		},
		{
			name:         "wrong username rejected",
			cfgUser:      "admin",
			cfgPass:      "secret",
			reqUser:      "someoneelse",
			reqPass:      "secret",
			setAuth:      true,
			wantStatus:   http.StatusUnauthorized,
			wantReached:  false,
			wantWWWAuthn: true,
		},
		{
			name:         "only username configured (empty password) — incorrect password still rejected",
			cfgUser:      "admin",
			cfgPass:      "",
			reqUser:      "admin",
			reqPass:      "anything",
			setAuth:      true,
			wantStatus:   http.StatusUnauthorized,
			wantReached:  false,
			wantWWWAuthn: true,
		},
		{
			name:        "only username configured and request has both — still requires password match",
			cfgUser:     "admin",
			cfgPass:     "",
			reqUser:     "admin",
			reqPass:     "",
			setAuth:     true,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:         "empty username but password set — auth is still enabled if password is set",
			cfgUser:      "",
			cfgPass:      "secret",
			reqUser:      "",
			reqPass:      "secret",
			setAuth:      true,
			wantStatus:   http.StatusOK,
			wantReached:  true,
			wantWWWAuthn: false,
		},
		{
			name:         "password mismatch when only password is configured",
			cfgUser:      "",
			cfgPass:      "secret",
			reqUser:      "",
			reqPass:      "wrong",
			setAuth:      true,
			wantStatus:   http.StatusUnauthorized,
			wantReached:  false,
			wantWWWAuthn: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			h := basicAuthMiddleware(tc.cfgUser, tc.cfgPass, okHandler(&reached))

			req := httptest.NewRequest("GET", "/", nil)
			if tc.setAuth {
				req.SetBasicAuth(tc.reqUser, tc.reqPass)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if reached != tc.wantReached {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantReached)
			}
			if tc.wantWWWAuthn && rec.Header().Get("WWW-Authenticate") == "" {
				t.Errorf("expected WWW-Authenticate header to be set on 401")
			}
		})
	}
}

func TestBearerAuthMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		cfgToken    string
		authHeader  string
		setHeader   bool
		wantStatus  int
		wantReached bool
	}{
		{
			name:        "auth disabled when no token configured",
			cfgToken:    "",
			setHeader:   false,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "correct token passes",
			cfgToken:    "s3cr3t-token",
			authHeader:  "Bearer s3cr3t-token",
			setHeader:   true,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "missing header rejected",
			cfgToken:    "s3cr3t-token",
			setHeader:   false,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
		{
			name:        "malformed header rejected",
			cfgToken:    "s3cr3t-token",
			authHeader:  "s3cr3t-token",
			setHeader:   true,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
		{
			name:        "wrong token rejected",
			cfgToken:    "s3cr3t-token",
			authHeader:  "Bearer wrong-token",
			setHeader:   true,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
		{
			name:        "basic auth header sent to bearer endpoint rejected",
			cfgToken:    "s3cr3t-token",
			authHeader:  "Basic dXNlcjpwYXNz",
			setHeader:   true,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
		{
			name:        "bearer with extra whitespace in prefix passes",
			cfgToken:    "token123",
			authHeader:  "Bearer token123",
			setHeader:   true,
			wantStatus:  http.StatusOK,
			wantReached: true,
		},
		{
			name:        "empty bearer token rejected",
			cfgToken:    "s3cr3t-token",
			authHeader:  "Bearer ",
			setHeader:   true,
			wantStatus:  http.StatusUnauthorized,
			wantReached: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			h := bearerAuthMiddleware(tc.cfgToken, okHandler(&reached))

			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tc.setHeader {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if reached != tc.wantReached {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantReached)
			}
		})
	}
}
