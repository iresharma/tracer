package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"), 4000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustInsert(t *testing.T, s *Store, entries []model.LogEntry) {
	t.Helper()
	if err := s.insertBatch(entries); err != nil {
		t.Fatalf("insertBatch: %v", err)
	}
}

func TestInsertAndBrowse(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	entries := []model.LogEntry{
		{TS: now.UnixMicro(), Namespace: "default", Pod: "app-1", PodUID: "u1", Container: "app", Node: "n1", Stream: "stdout", TraceID: "t1", IsJSON: true, Raw: `{"msg":"hello","trace_id":"t1"}`},
		{TS: now.Add(time.Second).UnixMicro(), Namespace: "default", Pod: "app-1", PodUID: "u1", Container: "app", Node: "n1", Stream: "stdout", Raw: "plain line, no json"},
	}
	mustInsert(t, s, entries)

	rows, err := s.Browse(BrowseFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// Browse orders DESC by id, so the plain line (inserted second) comes first.
	if rows[0].Raw != entries[1].Raw {
		t.Errorf("expected most recent row first, got %q", rows[0].Raw)
	}
}

func TestTraceByID(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	entries := []model.LogEntry{
		{TS: now.UnixMicro(), Namespace: "default", Pod: "svc-a", Container: "svc-a", Node: "n1", Stream: "stdout", TraceID: "abc", IsJSON: true, Raw: `{"trace_id":"abc","service":"a"}`},
		{TS: now.Add(2 * time.Second).UnixMicro(), Namespace: "default", Pod: "svc-b", Container: "svc-b", Node: "n1", Stream: "stdout", TraceID: "abc", IsJSON: true, Raw: `{"trace_id":"abc","service":"b"}`},
		{TS: now.Add(time.Second).UnixMicro(), Namespace: "default", Pod: "svc-c", Container: "svc-c", Node: "n1", Stream: "stdout", TraceID: "other", IsJSON: true, Raw: `{"trace_id":"other"}`},
	}
	mustInsert(t, s, entries)

	rows, err := s.TraceByID("abc", 2)
	if err != nil {
		t.Fatalf("TraceByID: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for trace abc, got %d", len(rows))
	}
	if rows[0].Pod != "svc-a" || rows[1].Pod != "svc-b" {
		t.Errorf("expected time-ordered svc-a then svc-b, got %s then %s", rows[0].Pod, rows[1].Pod)
	}
}

func TestPruneOlderThan(t *testing.T) {
	s := newTestStore(t)
	old := time.Now().AddDate(0, 0, -20)
	recent := time.Now()

	mustInsert(t, s, []model.LogEntry{
		{TS: old.UnixMicro(), Namespace: "ns", Pod: "p", Container: "c", Node: "n", Stream: "stdout", Raw: "old"},
		{TS: recent.UnixMicro(), Namespace: "ns", Pod: "p", Container: "c", Node: "n", Stream: "stdout", Raw: "new"},
	})

	partitions, err := s.ListPartitions()
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(partitions) != 2 {
		t.Fatalf("expected 2 partitions before prune, got %d", len(partitions))
	}

	dropped, err := s.PruneOlderThan(10)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if len(dropped) != 1 {
		t.Fatalf("expected 1 dropped partition, got %d (%v)", len(dropped), dropped)
	}

	partitions, err = s.ListPartitions()
	if err != nil {
		t.Fatalf("ListPartitions after prune: %v", err)
	}
	if len(partitions) != 1 {
		t.Fatalf("expected 1 partition after prune, got %d", len(partitions))
	}
}

func TestValidateDayRejectsBadInput(t *testing.T) {
	bad := []string{"", "abc", "2026-09-03", "20260903; DROP TABLE day_partitions;--"}
	for _, b := range bad {
		if err := validateDay(b); err == nil {
			t.Errorf("expected validateDay(%q) to fail", b)
		}
	}
	if err := validateDay("20260903"); err != nil {
		t.Errorf("expected valid day to pass, got %v", err)
	}
}
