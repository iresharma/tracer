package store

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/iresharma/tracer/internal/model"
)

// LogRow is a stored log entry plus its database id, used for pagination.
type LogRow struct {
	ID int64
	model.LogEntry
}

func scanRows(rows *sql.Rows) ([]LogRow, error) {
	var out []LogRow
	for rows.Next() {
		var r LogRow
		var isJSON int
		if err := rows.Scan(&r.ID, &r.TS, &r.Namespace, &r.Pod, &r.PodUID, &r.Container, &r.Node, &r.Stream, &r.TraceID, &isJSON, &r.Raw); err != nil {
			return nil, err
		}
		r.IsJSON = isJSON != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// partitionExists reports whether a day-table currently exists, without
// erroring when it doesn't (e.g. a candidate day with no logs yet).
func (s *Store) partitionExists(day string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM day_partitions WHERE day = ?`, day).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

const selectCols = "id, ts, namespace, pod, pod_uid, container, node, stream, trace_id, is_json, raw"

// TraceByID fans out to the last windowDays day-tables (today and back),
// queries each using the partial trace_id index, and merges+sorts the
// results in Go — trace result sets are small enough that this is simpler
// than building dynamic multi-table UNION SQL.
func (s *Store) TraceByID(traceID string, windowDays int) ([]LogRow, error) {
	if windowDays <= 0 {
		windowDays = 2
	}
	var all []LogRow
	now := time.Now().UTC()
	for i := 0; i < windowDays; i++ {
		day := dayKey(now.AddDate(0, 0, -i))
		ok, err := s.partitionExists(day)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err := validateDay(day); err != nil {
			return nil, err
		}
		q := fmt.Sprintf(`SELECT %s FROM %s WHERE trace_id = ? ORDER BY ts ASC LIMIT 5000`, selectCols, tableName(day))
		rows, err := s.db.Query(q, traceID)
		if err != nil {
			return nil, fmt.Errorf("query trace on %s: %w", day, err)
		}
		got, err := scanRows(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		all = append(all, got...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].TS < all[j].TS })
	return all, nil
}

// BrowseFilter parameterizes the browse/search query.
type BrowseFilter struct {
	Day       string // YYYY-MM-DD (UI-facing); empty = today
	Namespace string
	Pod       string
	Container string
	Query     string // free-text substring match against raw
	Cursor    int64  // last-seen id; 0 = first page
	Limit     int
}

// Browse queries a single day-table (the common case). When the effective
// UTC day differs from an adjacent day near a boundary, callers needing
// multi-day ranges should call Browse per-day and merge; MVP scope keeps
// the UI itself single-day.
func (s *Store) Browse(f BrowseFilter) ([]LogRow, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	day := f.Day
	if day == "" {
		day = dayKey(time.Now())
	} else {
		day = normalizeDay(day)
	}
	if err := validateDay(day); err != nil {
		return nil, err
	}
	ok, err := s.partitionExists(day)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	q := fmt.Sprintf(`SELECT %s FROM %s WHERE
		(? = '' OR namespace = ?) AND
		(? = '' OR pod = ?) AND
		(? = '' OR container = ?) AND
		(? = '' OR raw LIKE '%%' || ? || '%%') AND
		(? = 0 OR id < ?)
		ORDER BY id DESC LIMIT ?`, selectCols, tableName(day))

	rows, err := s.db.Query(q,
		f.Namespace, f.Namespace,
		f.Pod, f.Pod,
		f.Container, f.Container,
		f.Query, f.Query,
		f.Cursor, f.Cursor,
		f.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// normalizeDay converts a UI-facing "YYYY-MM-DD" into the internal
// "YYYYMMDD" partition key; if already in that form, returns it unchanged.
func normalizeDay(day string) string {
	if t, err := time.Parse("2006-01-02", day); err == nil {
		return dayKey(t)
	}
	return day
}

// Facets holds distinct values used to populate UI filter dropdowns.
type Facets struct {
	Namespaces []string   `json:"namespaces"`
	Pods       []PodFacet `json:"pods"`
	Containers []string   `json:"containers"`
}

type PodFacet struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
}

var (
	facetsMu    sync.Mutex
	facetsCache *Facets
	facetsAt    time.Time
)

const facetsTTL = 45 * time.Second

// GetFacets computes distinct namespace/pod/container values from today's
// (and, near midnight, yesterday's) partition, cached briefly in-process.
// Deliberately not a maintained side-table, to avoid write amplification on
// the hot ingest path.
func (s *Store) GetFacets() (*Facets, error) {
	facetsMu.Lock()
	if facetsCache != nil && time.Since(facetsAt) < facetsTTL {
		f := facetsCache
		facetsMu.Unlock()
		return f, nil
	}
	facetsMu.Unlock()

	nsSet := map[string]struct{}{}
	containerSet := map[string]struct{}{}
	podSet := map[PodFacet]struct{}{}

	now := time.Now().UTC()
	days := []string{dayKey(now)}
	if now.Hour() == 0 {
		days = append(days, dayKey(now.AddDate(0, 0, -1)))
	}
	for _, day := range days {
		if err := validateDay(day); err != nil {
			return nil, err
		}
		ok, err := s.partitionExists(day)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		q := fmt.Sprintf(`SELECT DISTINCT namespace, pod, container FROM %s`, tableName(day))
		rows, err := s.db.Query(q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var ns, pod, container string
			if err := rows.Scan(&ns, &pod, &container); err != nil {
				rows.Close()
				return nil, err
			}
			nsSet[ns] = struct{}{}
			containerSet[container] = struct{}{}
			podSet[PodFacet{Namespace: ns, Pod: pod}] = struct{}{}
		}
		rows.Close()
	}

	f := &Facets{}
	for ns := range nsSet {
		f.Namespaces = append(f.Namespaces, ns)
	}
	for c := range containerSet {
		f.Containers = append(f.Containers, c)
	}
	for p := range podSet {
		f.Pods = append(f.Pods, p)
	}
	sort.Strings(f.Namespaces)
	sort.Strings(f.Containers)
	sort.Slice(f.Pods, func(i, j int) bool {
		if f.Pods[i].Namespace != f.Pods[j].Namespace {
			return f.Pods[i].Namespace < f.Pods[j].Namespace
		}
		return f.Pods[i].Pod < f.Pods[j].Pod
	})

	facetsMu.Lock()
	facetsCache = f
	facetsAt = time.Now()
	facetsMu.Unlock()
	return f, nil
}
