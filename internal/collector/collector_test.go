package collector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
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
