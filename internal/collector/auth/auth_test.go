package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestRequireIngestToken(t *testing.T) {
	tests := []struct {
		name       string
		token      string // configured token; empty means auth disabled
		authHeader string // Authorization header sent by the caller
		wantCode   int
	}{
		{
			name:       "no token configured allows any request",
			token:      "",
			authHeader: "",
			wantCode:   http.StatusOK,
		},
		{
			name:       "missing header rejected",
			token:      "s3cret",
			authHeader: "",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "wrong token rejected",
			token:      "s3cret",
			authHeader: "Bearer wrong",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "correct token accepted",
			token:      "s3cret",
			authHeader: "Bearer s3cret",
			wantCode:   http.StatusOK,
		},
		{
			name:       "prefix-only match rejected (not just a substring check)",
			token:      "s3cret",
			authHeader: "Bearer s3cretextra",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "basic auth header rejected when token required",
			token:      "s3cret",
			authHeader: "Basic dXNlcjpwYXNz",
			wantCode:   http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := RequireIngestToken(tc.token, okHandler())

			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("expected status %d, got %d: %s", tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRequireBasicAuth(t *testing.T) {
	tests := []struct {
		name     string
		user     string // configured username; empty (with pass) means auth disabled
		pass     string // configured password
		reqUser  string // username sent by the caller, if any
		reqPass  string // password sent by the caller, if any
		sendAuth bool   // whether to set the Authorization header at all
		wantCode int
	}{
		{
			name:     "no credentials configured allows any request",
			user:     "",
			pass:     "",
			sendAuth: false,
			wantCode: http.StatusOK,
		},
		{
			name:     "missing header rejected",
			user:     "admin",
			pass:     "hunter2",
			sendAuth: false,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "wrong password rejected",
			user:     "admin",
			pass:     "hunter2",
			reqUser:  "admin",
			reqPass:  "wrong",
			sendAuth: true,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "wrong username rejected",
			user:     "admin",
			pass:     "hunter2",
			reqUser:  "someone-else",
			reqPass:  "hunter2",
			sendAuth: true,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "correct credentials accepted",
			user:     "admin",
			pass:     "hunter2",
			reqUser:  "admin",
			reqPass:  "hunter2",
			sendAuth: true,
			wantCode: http.StatusOK,
		},
		{
			name:     "only username configured (password empty) disables auth",
			user:     "admin",
			pass:     "",
			sendAuth: false,
			wantCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := RequireBasicAuth(tc.user, tc.pass, okHandler())

			req := httptest.NewRequest("GET", "/", nil)
			if tc.sendAuth {
				req.SetBasicAuth(tc.reqUser, tc.reqPass)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("expected status %d, got %d: %s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantCode == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got == "" {
					t.Errorf("expected WWW-Authenticate header on 401, got none")
				}
			}
		})
	}
}
