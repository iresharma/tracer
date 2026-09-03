package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"time"
)

var dayRe = regexp.MustCompile(`^\d{8}$`)

// dayKey returns the YYYYMMDD partition key for t, in UTC.
func dayKey(t time.Time) string {
	return t.UTC().Format("20060102")
}

// validateDay guards against SQL injection via table-name interpolation:
// SQLite cannot parameterize table names, so every day key used to build a
// table name must pass this check first.
func validateDay(day string) error {
	if !dayRe.MatchString(day) {
		return fmt.Errorf("invalid day partition key %q", day)
	}
	return nil
}

func tableName(day string) string {
	return "logs_" + day
}

func indexNames(day string) (ts, nsPod, trace string) {
	return "idx_" + day + "_ts", "idx_" + day + "_ns_pod_ts", "idx_" + day + "_trace"
}

// ensurePartition creates the day-table (and its indexes) and registers it
// in day_partitions if it doesn't already exist. Safe to call on every
// insert batch; CREATE TABLE IF NOT EXISTS makes the common case a no-op.
func ensurePartition(execer *sql.Tx, day string) error {
	if err := validateDay(day); err != nil {
		return err
	}
	table := tableName(day)
	tsIdx, nsPodIdx, traceIdx := indexNames(day)

	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			ts         INTEGER NOT NULL,
			namespace  TEXT NOT NULL,
			pod        TEXT NOT NULL,
			pod_uid    TEXT NOT NULL,
			container  TEXT NOT NULL,
			node       TEXT NOT NULL,
			stream     TEXT NOT NULL,
			trace_id   TEXT NOT NULL DEFAULT '',
			is_json    INTEGER NOT NULL DEFAULT 0,
			raw        TEXT NOT NULL
		)`, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(ts)`, tsIdx, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(namespace, pod, ts)`, nsPodIdx, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(trace_id) WHERE trace_id != ''`, traceIdx, table),
	}
	for _, stmt := range stmts {
		if _, err := execer.Exec(stmt); err != nil {
			return fmt.Errorf("ensure partition %s: %w", day, err)
		}
	}
	if _, err := execer.Exec(
		`INSERT INTO day_partitions(day, created_at) VALUES(?, ?) ON CONFLICT(day) DO NOTHING`,
		day, time.Now().UTC().Unix(),
	); err != nil {
		return fmt.Errorf("register partition %s: %w", day, err)
	}
	return nil
}

// ListPartitions returns all known day keys, oldest first.
func (s *Store) ListPartitions() ([]string, error) {
	rows, err := s.db.Query(`SELECT day FROM day_partitions ORDER BY day ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// PruneOlderThan drops any day-table older than the retention window
// (retentionDays back from now, UTC) via DROP TABLE — cheap and
// non-fragmenting compared to DELETE + VACUUM.
func (s *Store) PruneOlderThan(retentionDays int) (dropped []string, err error) {
	cutoff := dayKey(time.Now().AddDate(0, 0, -retentionDays))
	days, err := s.ListPartitions()
	if err != nil {
		return nil, err
	}
	for _, day := range days {
		if day >= cutoff {
			continue
		}
		if err := validateDay(day); err != nil {
			return dropped, err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return dropped, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tableName(day))); err != nil {
			tx.Rollback()
			return dropped, fmt.Errorf("drop partition %s: %w", day, err)
		}
		if _, err := tx.Exec(`DELETE FROM day_partitions WHERE day = ?`, day); err != nil {
			tx.Rollback()
			return dropped, fmt.Errorf("deregister partition %s: %w", day, err)
		}
		if err := tx.Commit(); err != nil {
			return dropped, err
		}
		dropped = append(dropped, day)
	}
	return dropped, nil
}
