package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iresharma/tracer/internal/collector/config"
)

// TestRoutesWithoutAuthAreOpen verifies that health/readiness/metrics/static
// routes are NOT protected by auth middleware (they bypass basicAuth).
func TestRoutesWithoutAuthAreOpen(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		// These should be unprotected
		{"healthz no auth", "GET", "/healthz", http.StatusOK},
		{"readyz no auth", "GET", "/readyz", http.StatusOK},
		{"metrics no auth", "GET", "/metrics", http.StatusOK},
		{"static css no auth", "GET", "/static/app.css", http.StatusNotFound}, // not found but not 401
	}

	// Create a minimal server with auth enabled
	cfg := config.Config{
		HTTPAddr:        ":0",
		DBPath:          ":memory:",
		AuthUsername:    "admin",
		AuthPassword:    "secret",
		IngestAuthToken: "s3cr3t",
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer s.store.Close()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("route %s %s was protected by auth when it should not be", tc.method, tc.path)
			}
			if rec.Code != tc.wantStatus {
				t.Logf("route %s %s returned %d, expected %d (but not 401 so OK)", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestProtectedRoutesRequireBasicAuth verifies that UI and JSON API routes
// are protected by basic auth when enabled.
func TestProtectedRoutesRequireBasicAuth(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		withAuth   bool
		wantStatus int
	}{
		// UI routes require auth
		{"GET / without auth", "GET", "/", false, http.StatusUnauthorized},
		{"GET / with auth", "GET", "/", true, http.StatusOK},
		{"GET /logs without auth", "GET", "/logs", false, http.StatusUnauthorized},
		{"GET /logs with auth", "GET", "/logs", true, http.StatusOK},
		{"GET /query without auth", "GET", "/query", false, http.StatusUnauthorized},
		{"GET /query with auth", "GET", "/query", true, http.StatusOK},

		// JSON API routes require auth
		{"GET /api/v1/logs without auth", "GET", "/api/v1/logs", false, http.StatusUnauthorized},
		{"GET /api/v1/logs with auth", "GET", "/api/v1/logs", true, http.StatusOK},
	}

	cfg := config.Config{
		HTTPAddr:        ":0",
		DBPath:          ":memory:",
		AuthUsername:    "admin",
		AuthPassword:    "secret",
		IngestAuthToken: "token", // not relevant to these basic-auth tests
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer s.store.Close()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.withAuth {
				req.SetBasicAuth("admin", "secret")
			}
			rec := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(rec, req)

			// We expect either 200 (success) or 401 (auth failure).
			// Some routes might return 404 for other reasons (route not found),
			// but if auth is enforced, a 401 means the auth middleware rejected it.
			if rec.Code != tc.wantStatus {
				t.Fatalf("route %s %s returned %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestIngestEndpointRequiresBearerAuth verifies that POST /api/v1/logs
// is protected by bearer token auth when enabled, not basic auth.
func TestIngestEndpointRequiresBearerAuth(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		withBasicAuth bool
		wantStatus    int
		description   string
	}{
		{"no auth header", "", false, http.StatusUnauthorized, "missing bearer token"},
		{"wrong bearer token", "Bearer wrong", false, http.StatusUnauthorized, "wrong token rejected"},
		{"basic auth to ingest", "", true, http.StatusUnauthorized, "basic auth doesn't work for ingest"},
	}

	cfg := config.Config{
		HTTPAddr:        ":0",
		DBPath:          ":memory:",
		AuthUsername:    "admin",
		AuthPassword:    "secret",
		IngestAuthToken: "s3cr3t",
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer s.store.Close()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/logs", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			if tc.withBasicAuth {
				req.SetBasicAuth("admin", "secret")
			}
			rec := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: got %d, want %d", tc.description, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestAuthDisabledWhenNotConfigured verifies that routes are unprotected
// when auth credentials are not configured.
func TestAuthDisabledWhenNotConfigured(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET / without creds set", "GET", "/"},
		{"GET /logs without creds set", "GET", "/logs"},
		{"GET /query without creds set", "GET", "/query"},
		{"GET /api/v1/logs without creds set", "GET", "/api/v1/logs"},
	}

	// Create server with auth disabled (empty creds)
	cfg := config.Config{
		HTTPAddr:        ":0",
		DBPath:          ":memory:",
		AuthUsername:    "", // auth disabled
		AuthPassword:    "", // auth disabled
		IngestAuthToken: "", // ingest auth disabled
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer s.store.Close()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// Don't set any auth headers
			rec := httptest.NewRecorder()
			s.http.Handler.ServeHTTP(rec, req)

			// These should NOT return 401 when auth is disabled
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("route %s %s returned 401 even though auth is disabled", tc.method, tc.path)
			}
		})
	}
}

// BenchmarkBasicAuthMiddleware measures the performance impact of the
// basic auth middleware on successful requests (the happy path).
func BenchmarkBasicAuthMiddleware(b *testing.B) {
	var reached bool
	h := basicAuthMiddleware("admin", "secret", okHandler(&reached))

	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("admin", "secret")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}

// BenchmarkBearerAuthMiddleware measures the performance impact of the
// bearer auth middleware on successful requests (the happy path).
func BenchmarkBearerAuthMiddleware(b *testing.B) {
	var reached bool
	h := bearerAuthMiddleware("s3cr3t-token", okHandler(&reached))

	req := httptest.NewRequest("POST", "/api/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t-token")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}
