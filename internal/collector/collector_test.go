package collector

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/iresharma/tracer/internal/collector/config"
)

func TestInstrumentRecordsRequestsByRouteAndStatus(t *testing.T) {
	handler := instrument("GET /test/instrument", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest("GET", "/test/instrument", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected wrapped handler's status to pass through, got %d", rec.Code)
	}

	body := scrapeMetrics(t)
	if !strings.Contains(body, `tracer_collector_http_requests_total{code="418",route="GET /test/instrument"}`) {
		t.Errorf("expected a requests_total series for this route/code, got:\n%s", body)
	}
}

func TestInstrumentDefaultsStatusTo200WhenHandlerNeverCallsWriteHeader(t *testing.T) {
	handler := instrument("GET /test/default-status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) // no explicit WriteHeader call
	}))

	req := httptest.NewRequest("GET", "/test/default-status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := scrapeMetrics(t)
	if !strings.Contains(body, `tracer_collector_http_requests_total{code="200",route="GET /test/default-status"}`) {
		t.Errorf("expected default 200 status to be recorded, got:\n%s", body)
	}
}

func scrapeMetrics(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestBasicAuthMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name           string
		authHeader     string
		wantStatus     int
		wantWWWAuthHdr bool
	}{
		{name: "missing credentials", authHeader: "", wantStatus: http.StatusUnauthorized, wantWWWAuthHdr: true},
		{name: "wrong credentials", authHeader: basicAuthHeader("wrong", "wrong"), wantStatus: http.StatusUnauthorized, wantWWWAuthHdr: true},
		{name: "wrong password only", authHeader: basicAuthHeader("admin", "wrong"), wantStatus: http.StatusUnauthorized, wantWWWAuthHdr: true},
		{name: "correct credentials", authHeader: basicAuthHeader("admin", "secret"), wantStatus: http.StatusOK, wantWWWAuthHdr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := basicAuthMiddleware("admin", "secret", ok)
			req := httptest.NewRequest("GET", "/", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			gotHdr := rec.Header().Get("WWW-Authenticate") != ""
			if gotHdr != tc.wantWWWAuthHdr {
				t.Errorf("expected WWW-Authenticate header present=%v, got %q", tc.wantWWWAuthHdr, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestBearerAuthMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "missing token", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", authHeader: "Bearer wrong-token", wantStatus: http.StatusUnauthorized},
		{name: "malformed header", authHeader: "token-only", wantStatus: http.StatusUnauthorized},
		{name: "correct token", authHeader: "Bearer good-token", wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := bearerAuthMiddleware("good-token", ok)
			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Errorf("expected no WWW-Authenticate header on bearer-protected route, got %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestNewRefusesToStartWithoutAuthConfigured(t *testing.T) {
	base := func() config.Config {
		return config.Config{
			HTTPAddr:      ":0",
			DBPath:        filepath.Join(t.TempDir(), "test.db"),
			SQLiteCacheKB: 4000,
			AuthUsername:  "admin",
			AuthPassword:  "secret",
			IngestToken:   "good-token",
		}
	}

	cases := []struct {
		name    string
		mutate  func(c config.Config) config.Config
		wantErr bool
	}{
		{name: "all set", mutate: func(c config.Config) config.Config { return c }, wantErr: false},
		{name: "missing username", mutate: func(c config.Config) config.Config { c.AuthUsername = ""; return c }, wantErr: true},
		{name: "missing password", mutate: func(c config.Config) config.Config { c.AuthPassword = ""; return c }, wantErr: true},
		{name: "missing ingest token", mutate: func(c config.Config) config.Config { c.IngestToken = ""; return c }, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.mutate(base())
			cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
			srv, err := New(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected New to return an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected New to succeed, got: %v", err)
			}
			srv.store.Close()
		})
	}
}

// TestUnauthenticatedRoutesStayReachable asserts that the routes registered
// directly on the mux (bypassing route()/auth) remain callable without any
// credentials, and that the UI/query-API/ingest routes now require the
// appropriate credential.
func TestUnauthenticatedRoutesStayReachable(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:      ":0",
		DBPath:        filepath.Join(t.TempDir(), "test.db"),
		SQLiteCacheKB: 4000,
		AuthUsername:  "admin",
		AuthPassword:  "secret",
		IngestToken:   "good-token",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.store.Close()

	mux := srv.http.Handler

	cases := []struct {
		name       string
		method     string
		path       string
		authHeader string
		wantStatus int
	}{
		{name: "healthz no auth", method: "GET", path: "/healthz", wantStatus: http.StatusOK},
		{name: "readyz no auth", method: "GET", path: "/readyz", wantStatus: http.StatusOK},
		{name: "metrics no auth", method: "GET", path: "/metrics", wantStatus: http.StatusOK},
		{name: "static no auth", method: "GET", path: "/static/app.css", wantStatus: http.StatusOK},
		{name: "ui index without basic auth is rejected", method: "GET", path: "/", wantStatus: http.StatusUnauthorized},
		{name: "ui index with basic auth succeeds", method: "GET", path: "/", authHeader: basicAuthHeader("admin", "secret"), wantStatus: http.StatusOK},
		{name: "query api without basic auth is rejected", method: "GET", path: "/api/v1/logs", wantStatus: http.StatusUnauthorized},
		{name: "query api with basic auth succeeds", method: "GET", path: "/api/v1/logs", authHeader: basicAuthHeader("admin", "secret"), wantStatus: http.StatusOK},
		{name: "ingest without bearer token is rejected", method: "POST", path: "/api/v1/logs", wantStatus: http.StatusUnauthorized},
		{name: "ingest with wrong basic auth still rejected", method: "POST", path: "/api/v1/logs", authHeader: basicAuthHeader("admin", "secret"), wantStatus: http.StatusUnauthorized},
		{name: "ingest with bearer token succeeds", method: "POST", path: "/api/v1/logs", authHeader: "Bearer good-token", wantStatus: http.StatusAccepted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.method == "POST" {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"agent_id":"a","entries":[]}`))
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

// basicAuthHeader builds a Basic Auth "Authorization" header value for
// tests without depending on a live *http.Request/http.Client.
func basicAuthHeader(username, password string) string {
	req := &http.Request{Header: http.Header{}}
	req.SetBasicAuth(username, password)
	return req.Header.Get("Authorization")
}
