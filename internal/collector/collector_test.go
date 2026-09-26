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

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		HTTPAddr:          ":0",
		DBPath:            filepath.Join(t.TempDir(), "test.db"),
		RetentionDays:     10,
		TraceWindowDays:   2,
		SQLiteCacheKB:     4000,
		IngestChanCap:     10,
		WriterFlushEvery:  200,
		WriterFlushAtSize: 500,
	}
}

func TestNewReturnsErrorWhenAuthEnabledWithMissingCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  func(config.Config) config.Config
	}{
		{"missing username", func(c config.Config) config.Config {
			c.AuthEnabled = true
			c.AuthPassword = "pass"
			c.AuthIngestToken = "token"
			return c
		}},
		{"missing password", func(c config.Config) config.Config {
			c.AuthEnabled = true
			c.AuthUsername = "user"
			c.AuthIngestToken = "token"
			return c
		}},
		{"missing ingest token", func(c config.Config) config.Config {
			c.AuthEnabled = true
			c.AuthUsername = "user"
			c.AuthPassword = "pass"
			return c
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg(testConfig(t))
			srv, err := New(cfg)
			if err == nil {
				t.Fatalf("expected New to fail fast on missing auth credentials")
			}
			if srv != nil {
				t.Errorf("expected nil server on error, got %+v", srv)
			}
		})
	}
}

func TestNewLeavesHealthzReadyzMetricsUnauthenticatedWhenAuthEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthEnabled = true
	cfg.AuthUsername = "user"
	cfg.AuthPassword = "pass"
	cfg.AuthIngestToken = "token"

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.store.Close() })

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("expected %s to remain unauthenticated, got 401", path)
		}
	}
}

func TestNewProtectsUIRouteWithBasicAuthWhenAuthEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthEnabled = true
	cfg.AuthUsername = "admin"
	cfg.AuthPassword = "secret"
	cfg.AuthIngestToken = "token"

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.store.Close() })

	tests := []struct {
		name     string
		path     string
		method   string
		username string
		password string
		expect   int
	}{
		{"root no auth", "/", "GET", "", "", http.StatusUnauthorized},
		{"root with auth", "/", "GET", "admin", "secret", http.StatusOK},
		{"logs no auth", "/logs", "GET", "", "", http.StatusUnauthorized},
		{"logs with auth", "/logs", "GET", "admin", "secret", http.StatusOK},
		{"static no auth", "/static/app.css", "GET", "", "", http.StatusUnauthorized},
		{"api trace no auth", "/api/v1/trace/123", "GET", "", "", http.StatusUnauthorized},
		{"api logs no auth", "/api/v1/logs", "GET", "", "", http.StatusUnauthorized},
		{"api facets no auth", "/api/v1/facets", "GET", "", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.username != "" || tt.password != "" {
				req.SetBasicAuth(tt.username, tt.password)
			}
			rec := httptest.NewRecorder()
			srv.http.Handler.ServeHTTP(rec, req)
			if rec.Code != tt.expect {
				t.Errorf("expected %d, got %d", tt.expect, rec.Code)
			}
		})
	}
}

func TestNewProtectsIngestWithTokenAuthWhenAuthEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthEnabled = true
	cfg.AuthUsername = "admin"
	cfg.AuthPassword = "secret"
	cfg.AuthIngestToken = "ingest-token"

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.store.Close() })

	tests := []struct {
		name            string
		authHeader      string
		expectStatus    int
		expectNoWWWAuth bool
	}{
		{"no auth", "", http.StatusUnauthorized, true},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized, true},
		{"valid token", "Bearer ingest-token", http.StatusAccepted, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/logs", strings.NewReader("{}"))
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			srv.http.Handler.ServeHTTP(rec, req)
			if rec.Code != tt.expectStatus {
				t.Errorf("expected status %d, got %d", tt.expectStatus, rec.Code)
			}
			if tt.expectNoWWWAuth && rec.Header().Get("WWW-Authenticate") != "" {
				t.Errorf("expected no WWW-Authenticate header for token auth, got %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

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
