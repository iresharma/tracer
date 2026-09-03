package ingest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/collector/store"
	"github.com/iresharma/tracer/internal/model"
)

func TestIngestHandlerAcceptsGzipBatch(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"), 4000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	w := store.NewWriter(s, 100, 50*time.Millisecond, 500)
	go w.Run()
	defer w.Stop()

	h := NewHandler(w)

	batch := model.Batch{
		AgentID: "node-1",
		Entries: []model.LogEntry{
			{TS: time.Now().UnixMicro(), Namespace: "default", Pod: "app", Container: "app", Node: "node-1", Stream: "stdout", Raw: "hello"},
		},
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gz).Encode(batch); err != nil {
		t.Fatalf("encode: %v", err)
	}
	gz.Close()

	req := httptest.NewRequest("POST", "/api/v1/logs", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// Give the writer's ticker a moment to flush.
	time.Sleep(150 * time.Millisecond)

	rows, err := s.Browse(store.BrowseFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row ingested, got %d", len(rows))
	}
	if rows[0].Raw != "hello" {
		t.Errorf("unexpected raw content: %q", rows[0].Raw)
	}
}

func TestIngestHandlerRejectsBadMethod(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/logs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
