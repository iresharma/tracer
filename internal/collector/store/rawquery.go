package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/iresharma/tracer/internal/collector/metrics"
)

const (
	maxQueryRows = 500
	queryTimeout = 10 * time.Second
)

// QueryResult is the generic tabular result of an ad-hoc SQL query, shaped
// for direct rendering — column names plus already-stringified rows, since
// the caller has no static schema to scan into.
type QueryResult struct {
	Columns   []string
	Rows      [][]string
	Truncated bool
}

// RunReadOnlyQuery executes an arbitrary, user-supplied SQL query against
// the database, for the UI's ad-hoc Query page.
//
// Safety is layered: the text check below rejects anything that isn't a
// single SELECT/WITH statement as a fast, friendly-error path, but the
// actual enforcement is that this runs against Store.roDB, a connection
// pool opened with PRAGMA query_only — so even a query that slipped past
// this check (or a future bug in it) still cannot write, DROP, or
// otherwise mutate the database; SQLite itself refuses the write.
//
// Results are capped at maxQueryRows and the query is cancelled after
// queryTimeout, so a pathological query (accidental cross join, unbounded
// scan) can't hold memory or a connection indefinitely on this
// resource-constrained pod.
func (s *Store) RunReadOnlyQuery(ctx context.Context, query string) (result *QueryResult, err error) {
	q := strings.TrimSpace(query)
	if q == "" {
		metrics.QueryTotal.WithLabelValues("rejected").Inc()
		return nil, fmt.Errorf("empty query")
	}
	q = strings.TrimSpace(strings.TrimSuffix(q, ";"))
	if strings.Contains(q, ";") {
		metrics.QueryTotal.WithLabelValues("rejected").Inc()
		return nil, fmt.Errorf("only a single statement is allowed")
	}
	lower := strings.ToLower(q)
	if !strings.HasPrefix(lower, "select") && !strings.HasPrefix(lower, "with") {
		metrics.QueryTotal.WithLabelValues("rejected").Inc()
		return nil, fmt.Errorf("only SELECT queries are allowed")
	}

	// From here, the query actually executes — record ok/error exactly
	// once based on the final named return, regardless of which of the
	// several error paths below fires.
	timer := prometheus.NewTimer(metrics.QueryDuration)
	defer func() {
		timer.ObserveDuration()
		if err != nil {
			metrics.QueryTotal.WithLabelValues("error").Inc()
		} else {
			metrics.QueryTotal.WithLabelValues("ok").Inc()
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := s.roDB.QueryContext(ctx, q)
	if err != nil {
		return nil, friendlyQueryError(err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result = &QueryResult{Columns: cols}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	for rows.Next() {
		if len(result.Rows) >= maxQueryRows {
			result.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = formatCell(v)
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, friendlyQueryError(err)
	}
	return result, nil
}

// friendlyQueryError appends an actionable hint to SQLite's raw error text
// for gotchas that are otherwise cryptic to a user typing ad-hoc SQL.
func friendlyQueryError(err error) error {
	if strings.Contains(err.Error(), "malformed JSON") {
		return fmt.Errorf("%w — add \"WHERE is_json = 1\" before applying json_extract/->> (not every log line is JSON, and SQLite errors rather than returning NULL for the ones that aren't)", err)
	}
	return err
}

func formatCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
