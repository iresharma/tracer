package queryapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/collector/store"
	"github.com/iresharma/tracer/internal/model"
)

func seededStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"), 4000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	w := store.NewWriter(s, 100, 10*time.Millisecond, 500)
	go w.Run()
	now := time.Now()
	entries := []model.LogEntry{
		{TS: now.UnixMicro(), Namespace: "default", Pod: "svc-a", Container: "svc-a", Node: "n1", Stream: "stdout", TraceID: "tid-1", IsJSON: true, Raw: `{"trace_id":"tid-1"}`},
		{TS: now.Add(time.Second).UnixMicro(), Namespace: "default", Pod: "svc-b", Container: "svc-b", Node: "n1", Stream: "stdout", TraceID: "tid-1", IsJSON: true, Raw: `{"trace_id":"tid-1"}`},
	}
	for _, e := range entries {
		w.Enqueue(e)
	}
	time.Sleep(100 * time.Millisecond)
	w.Stop()
	return s
}

func TestTraceHandler(t *testing.T) {
	s := seededStore(t)
	api := New(s, 2)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/trace/{id}", api.TraceHandler)

	req := httptest.NewRequest("GET", "/api/v1/trace/tid-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var rows []store.LogRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestFacetsHandler(t *testing.T) {
	s := seededStore(t)
	api := New(s, 2)

	req := httptest.NewRequest("GET", "/api/v1/facets", nil)
	rec := httptest.NewRecorder()
	api.FacetsHandler(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var f store.Facets
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(f.Namespaces) != 1 || f.Namespaces[0] != "default" {
		t.Errorf("unexpected namespaces: %v", f.Namespaces)
	}
}
