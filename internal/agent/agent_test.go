package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iresharma/tracer/internal/agent/metrics"
)

func TestMetricsMuxServesHealthz(t *testing.T) {
	mux := metricsMux()
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("unexpected body: %q", rec.Body.String())
	}
}

func TestMetricsMuxServesPrometheusFormat(t *testing.T) {
	// Touch a real metric so the registry has at least one series to render.
	metrics.TailerLinesTotal.Add(0)

	mux := metricsMux()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tracer_agent_tailer_lines_total") {
		t.Errorf("expected tracer_agent_tailer_lines_total in /metrics output, got:\n%s", body)
	}
}
