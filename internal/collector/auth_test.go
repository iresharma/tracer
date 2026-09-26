package collector

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sentinelHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("sentinel"))
	})
}

func TestRequireBasicAuth(t *testing.T) {
	cases := []struct {
		name           string
		username       string
		password       string
		authHeader     string
		wantStatus     int
		wantAuthHeader string
		wantBody       string
	}{
		{
			name:       "empty creds passthrough",
			username:   "",
			password:   "",
			authHeader: "",
			wantStatus: http.StatusOK,
			wantBody:   "sentinel",
		},
		{
			name:       "correct creds",
			username:   "alice",
			password:   "s3cret",
			authHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret")),
			wantStatus: http.StatusOK,
			wantBody:   "sentinel",
		},
		{
			name:           "wrong username",
			username:       "alice",
			password:       "s3cret",
			authHeader:     "Basic " + base64.StdEncoding.EncodeToString([]byte("mallory:s3cret")),
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Basic realm="tracer-collector"`,
		},
		{
			name:           "wrong password",
			username:       "alice",
			password:       "s3cret",
			authHeader:     "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:wrong")),
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Basic realm="tracer-collector"`,
		},
		{
			name:           "missing header",
			username:       "alice",
			password:       "s3cret",
			authHeader:     "",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Basic realm="tracer-collector"`,
		},
		{
			name:           "malformed header",
			username:       "alice",
			password:       "s3cret",
			authHeader:     "Basic not-valid-base64!!",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Basic realm="tracer-collector"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := requireBasicAuth(tc.username, tc.password)(sentinelHandler())

			req := httptest.NewRequest("GET", "/api/v1/trace/abc", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.wantAuthHeader {
				t.Errorf("expected WWW-Authenticate %q, got %q", tc.wantAuthHeader, got)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Errorf("expected body %q, got %q", tc.wantBody, rec.Body.String())
			}
		})
	}
}

func TestRequireBearerToken(t *testing.T) {
	cases := []struct {
		name           string
		token          string
		authHeader     string
		wantStatus     int
		wantAuthHeader string
		wantBody       string
	}{
		{
			name:       "empty token passthrough",
			token:      "",
			authHeader: "",
			wantStatus: http.StatusOK,
			wantBody:   "sentinel",
		},
		{
			name:       "correct token",
			token:      "s3cret-token",
			authHeader: "Bearer s3cret-token",
			wantStatus: http.StatusOK,
			wantBody:   "sentinel",
		},
		{
			name:           "wrong token",
			token:          "s3cret-token",
			authHeader:     "Bearer wrong-token",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer realm="tracer-collector"`,
		},
		{
			name:           "missing header",
			token:          "s3cret-token",
			authHeader:     "",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer realm="tracer-collector"`,
		},
		{
			name:           "malformed header no bearer prefix",
			token:          "s3cret-token",
			authHeader:     "s3cret-token",
			wantStatus:     http.StatusUnauthorized,
			wantAuthHeader: `Bearer realm="tracer-collector"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := requireBearerToken(tc.token)(sentinelHandler())

			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.wantAuthHeader {
				t.Errorf("expected WWW-Authenticate %q, got %q", tc.wantAuthHeader, got)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Errorf("expected body %q, got %q", tc.wantBody, rec.Body.String())
			}
		})
	}
}
