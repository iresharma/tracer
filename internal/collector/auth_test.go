package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireBasicAuth(t *testing.T) {
	cases := []struct {
		name           string
		username       string
		password       string
		setAuth        bool
		reqUser        string
		reqPass        string
		wantStatus     int
		wantWWWAuthHdr string
	}{
		{
			name:       "disabled when both empty",
			username:   "",
			password:   "",
			setAuth:    false,
			wantStatus: http.StatusOK,
		},
		{
			name:       "401 when no Authorization header",
			username:   "alice",
			password:   "secret",
			setAuth:    false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "401 when credentials wrong",
			username:   "alice",
			password:   "secret",
			setAuth:    true,
			reqUser:    "alice",
			reqPass:    "wrong",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "200 when credentials correct",
			username:   "alice",
			password:   "secret",
			setAuth:    true,
			reqUser:    "alice",
			reqPass:    "secret",
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := requireBasicAuth(tc.username, tc.password, okHandler())

			req := httptest.NewRequest("GET", "/", nil)
			if tc.setAuth {
				req.SetBasicAuth(tc.reqUser, tc.reqPass)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="tracer-collector"` {
					t.Errorf("expected WWW-Authenticate header, got %q", got)
				}
			}
		})
	}
}

func TestRequireBearerToken(t *testing.T) {
	cases := []struct {
		name       string
		token      string
		authHeader string
		setAuth    bool
		wantStatus int
	}{
		{
			name:       "disabled when token empty",
			token:      "",
			setAuth:    false,
			wantStatus: http.StatusOK,
		},
		{
			name:       "401 when no Authorization header",
			token:      "shh",
			setAuth:    false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "401 when token wrong",
			token:      "shh",
			authHeader: "Bearer nope",
			setAuth:    true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "200 when token correct",
			token:      "shh",
			authHeader: "Bearer shh",
			setAuth:    true,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := requireBearerToken(tc.token, okHandler())

			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tc.setAuth {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="tracer-collector"` {
					t.Errorf("expected WWW-Authenticate header, got %q", got)
				}
			}
		})
	}
}
