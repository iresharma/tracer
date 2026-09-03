package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/model"
)

func TestRunReadOnlyQuerySelectsData(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	mustInsert(t, s, []model.LogEntry{
		{TS: now.UnixMicro(), Namespace: "default", Pod: "app-1", Container: "app", Node: "n1", Stream: "stdout", TraceID: "t1", Raw: "hello"},
	})

	day := dayKey(now)
	result, err := s.RunReadOnlyQuery(context.Background(), "SELECT namespace, pod, raw FROM "+tableName(day))
	if err != nil {
		t.Fatalf("RunReadOnlyQuery: %v", err)
	}
	if len(result.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %v", result.Columns)
	}
	if len(result.Rows) != 1 || result.Rows[0][2] != "hello" {
		t.Fatalf("unexpected rows: %v", result.Rows)
	}
}

func TestRunReadOnlyQueryRejectsNonSelect(t *testing.T) {
	s := newTestStore(t)
	day := dayKey(time.Now())
	mustInsert(t, s, []model.LogEntry{{TS: time.Now().UnixMicro(), Namespace: "ns", Pod: "p", Container: "c", Node: "n", Stream: "stdout", Raw: "x"}})

	cases := []string{
		"DELETE FROM " + tableName(day),
		"DROP TABLE " + tableName(day),
		"UPDATE " + tableName(day) + " SET raw = 'x'",
		"SELECT 1; DROP TABLE " + tableName(day),
		"",
		"   ",
	}
	for _, q := range cases {
		if _, err := s.RunReadOnlyQuery(context.Background(), q); err == nil {
			t.Errorf("expected query %q to be rejected", q)
		}
	}
}

func TestRunReadOnlyQueryGivesFriendlyHintForMalformedJSON(t *testing.T) {
	s := newTestStore(t)
	day := dayKey(time.Now())
	mustInsert(t, s, []model.LogEntry{
		{TS: time.Now().UnixMicro(), Namespace: "ns", Pod: "p", Container: "c", Node: "n", Stream: "stdout", IsJSON: true, Raw: `{"level":"info"}`},
		{TS: time.Now().UnixMicro(), Namespace: "ns", Pod: "p", Container: "c", Node: "n", Stream: "stdout", IsJSON: false, Raw: "plain text, not json"},
	})

	// Without the is_json guard, hitting the non-JSON row mid-scan should
	// surface a friendly, actionable error rather than the raw SQLite text.
	_, err := s.RunReadOnlyQuery(context.Background(), "SELECT raw->>'level' FROM "+tableName(day))
	if err == nil {
		t.Fatal("expected an error querying a JSON field across mixed JSON/non-JSON rows")
	}
	if !strings.Contains(err.Error(), "is_json") {
		t.Errorf("expected error to hint at WHERE is_json = 1, got: %v", err)
	}

	// With the guard, it should succeed.
	result, err := s.RunReadOnlyQuery(context.Background(), "SELECT raw->>'level' FROM "+tableName(day)+" WHERE is_json = 1")
	if err != nil {
		t.Fatalf("RunReadOnlyQuery with is_json guard: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

// TestReadOnlyPoolRejectsWritesAtEngineLevel proves the actual safety
// boundary: even bypassing RunReadOnlyQuery's text-level SELECT check and
// issuing a write directly against the read-only pool, SQLite itself
// refuses it because that connection has PRAGMA query_only set.
func TestReadOnlyPoolRejectsWritesAtEngineLevel(t *testing.T) {
	s := newTestStore(t)
	day := dayKey(time.Now())
	mustInsert(t, s, []model.LogEntry{{TS: time.Now().UnixMicro(), Namespace: "ns", Pod: "p", Container: "c", Node: "n", Stream: "stdout", Raw: "x"}})

	_, err := s.roDB.Exec("DELETE FROM " + tableName(day))
	if err == nil {
		t.Fatal("expected the read-only pool to reject a write, but it succeeded")
	}

	rows, qerr := s.Browse(BrowseFilter{Limit: 10})
	if qerr != nil {
		t.Fatalf("Browse: %v", qerr)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the row to survive the rejected delete, got %d rows", len(rows))
	}
}
