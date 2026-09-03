// Package store implements the day-partitioned SQLite storage layer used by
// the collector. Retention is enforced by dropping whole day-tables instead
// of deleting rows, which keeps the hot path free of VACUUM/fragmentation
// costs on a memory-constrained pod.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store owns the single SQLite connection used for all reads and writes,
// plus a small separate read-only pool (roDB) used only for ad-hoc SQL
// queries from the UI's Query page. MVP deliberately uses one connection
// for the main path (SetMaxOpenConns(1)) for predictable memory usage; see
// collector design notes for the read-pool upgrade path.
type Store struct {
	db   *sql.DB
	roDB *sql.DB
}

// Open creates/opens the SQLite database at path, applies memory-conscious
// pragmas, and bootstraps the day_partitions registry table.
func Open(path string, cacheSizeKB int) (*Store, error) {
	if cacheSizeKB <= 0 {
		cacheSizeKB = 16000
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		fmt.Sprintf("PRAGMA cache_size = -%d", cacheSizeKB),
		"PRAGMA mmap_size = 67108864",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA busy_timeout = 5000",
		// Must be set before any table is created to take effect.
		"PRAGMA auto_vacuum = INCREMENTAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	// A second connection pool, opened with PRAGMA query_only set on every
	// connection, backs the ad-hoc SQL query feature. This is the real
	// safety boundary for that feature (enforced by SQLite itself) — a
	// text-level "must start with SELECT" check alone would be bypassable,
	// but a connection in query_only mode rejects any write at the engine
	// level regardless of what the query text says. WAL mode (set above,
	// and persisted in the database file) allows these read-only readers
	// to run concurrently with the writer without blocking it.
	roDSN := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=query_only(1)", path)
	roDB, err := sql.Open("sqlite", roDSN)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open sqlite (read-only pool): %w", err)
	}
	roDB.SetMaxOpenConns(2)

	s := &Store{db: db, roDB: roDB}
	if err := s.bootstrap(); err != nil {
		db.Close()
		roDB.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) bootstrap() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS day_partitions (
			day        TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("bootstrap day_partitions: %w", err)
	}
	return nil
}

// Close closes both underlying database connections.
func (s *Store) Close() error {
	s.roDB.Close()
	return s.db.Close()
}

// DB exposes the underlying *sql.DB for use by other files in this package.
func (s *Store) DB() *sql.DB {
	return s.db
}
