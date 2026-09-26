package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func passThroughHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestBasicAuthMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		user, pass string // configured credentials
		reqUser    string
		reqPass    string
		setAuth    bool
		wantStatus int
	}{
		{
			name:       "no credentials sent -> 401",
			user:       "admin",
			pass:       "secret",
			setAuth:    false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong username -> 401",
			user:       "admin",
			pass:       "secret",
			reqUser:    "nope",
			reqPass:    "secret",
			setAuth:    true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong password -> 401",
			user:       "admin",
			pass:       "secret",
			reqUser:    "admin",
			reqPass:    "nope",
			setAuth:    true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "correct credentials -> 200",
			user:       "admin",
			pass:       "secret",
			reqUser:    "admin",
			reqPass:    "secret",
			setAuth:    true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty configured user+pass -> always pass-through",
			user:       "",
			pass:       "",
			setAuth:    false,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := basicAuthMiddleware(passThroughHandler(), tt.user, tt.pass)

			req := httptest.NewRequest("GET", "/protected", nil)
			if tt.setAuth {
				req.SetBasicAuth(tt.reqUser, tt.reqPass)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
			if tt.wantStatus == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="tracer-collector"` {
					t.Errorf("expected WWW-Authenticate header, got %q", got)
				}
			}
		})
	}
}

func TestBearerAuthMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		token      string // configured token
		authHeader string
		setHeader  bool
		wantStatus int
	}{
		{
			name:       "missing header -> 401",
			token:      "s3cr3t",
			setHeader:  false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token -> 401",
			token:      "s3cr3t",
			authHeader: "Bearer wrong",
			setHeader:  true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "correct token -> 200",
			token:      "s3cr3t",
			authHeader: "Bearer s3cr3t",
			setHeader:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty configured token -> always pass-through",
			token:      "",
			setHeader:  false,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := bearerAuthMiddleware(passThroughHandler(), tt.token)

			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tt.setHeader {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}
