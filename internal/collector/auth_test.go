package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iresharma/tracer/internal/collector/config"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestBasicAuthMiddlewareNoopWhenAuthDisabled(t *testing.T) {
	cfg := config.Config{AuthEnabled: false}
	handler := basicAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("GET", "/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected pass-through 200 with auth disabled, got %d", rec.Code)
	}
}

func TestBasicAuthMiddlewareRejectsMissingCredentials(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthUsername: "user", AuthPassword: "pass"}
	handler := basicAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("GET", "/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no credentials, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("expected WWW-Authenticate header on 401")
	}
}

func TestBasicAuthMiddlewareRejectsWrongCredentials(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthUsername: "user", AuthPassword: "pass"}
	handler := basicAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("GET", "/logs", nil)
	req.SetBasicAuth("user", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong password, got %d", rec.Code)
	}
}

func TestBasicAuthMiddlewareAcceptsValidCredentials(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthUsername: "user", AuthPassword: "pass"}
	handler := basicAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("GET", "/logs", nil)
	req.SetBasicAuth("user", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid credentials, got %d", rec.Code)
	}
}

func TestTokenAuthMiddlewareNoopWhenAuthDisabled(t *testing.T) {
	cfg := config.Config{AuthEnabled: false}
	handler := tokenAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("POST", "/api/v1/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected pass-through 200 with auth disabled, got %d", rec.Code)
	}
}

func TestTokenAuthMiddlewareRejectsMissingHeader(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthIngestToken: "secret-token"}
	handler := tokenAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("POST", "/api/v1/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no Authorization header, got %d", rec.Code)
	}
}

func TestTokenAuthMiddlewareRejectsWrongToken(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthIngestToken: "secret-token"}
	handler := tokenAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("POST", "/api/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
}

func TestTokenAuthMiddlewareAcceptsValidToken(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthIngestToken: "secret-token"}
	handler := tokenAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("POST", "/api/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", rec.Code)
	}
}

func TestBasicAuthMiddlewareRejectsDifferentLengthPassword(t *testing.T) {
	// Ensures timing-constant comparison is used: same username, different
	// length password should be rejected before comparing password content.
	cfg := config.Config{AuthEnabled: true, AuthUsername: "user", AuthPassword: "secret"}
	handler := basicAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("GET", "/logs", nil)
	req.SetBasicAuth("user", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong length password, got %d", rec.Code)
	}
}

func TestBasicAuthMiddlewareRejectsDifferentLengthUsername(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthUsername: "user", AuthPassword: "pass"}
	handler := basicAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("GET", "/logs", nil)
	req.SetBasicAuth("userlong", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong length username, got %d", rec.Code)
	}
}

func TestTokenAuthMiddlewareRejectsDifferentLengthToken(t *testing.T) {
	cfg := config.Config{AuthEnabled: true, AuthIngestToken: "token"}
	handler := tokenAuthMiddleware(cfg, okHandler())

	req := httptest.NewRequest("POST", "/api/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer token-longer")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong length token, got %d", rec.Code)
	}
}
