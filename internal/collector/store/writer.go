package store

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/iresharma/tracer/internal/model"
)

// maxParamsPerInsert keeps each multi-row INSERT under SQLite's 999 bound
// parameter limit (11 columns per row -> 90 rows/statement is safely under).
const (
	colsPerRow          = 11
	maxRowsPerStatement = 90
)

// Writer is the single goroutine that owns all writes to the database. A
// single writer avoids SQLITE_BUSY contention from concurrent writers and
// amortizes fsync cost across batched transactions.
type Writer struct {
	store       *Store
	in          chan model.LogEntry
	flushEvery  time.Duration
	flushAtSize int
	done        chan struct{}
}

// NewWriter creates a writer that drains entries from an internally owned
// bounded channel of the given capacity.
func NewWriter(s *Store, chanCapacity int, flushEvery time.Duration, flushAtSize int) *Writer {
	if flushEvery <= 0 {
		flushEvery = 200 * time.Millisecond
	}
	if flushAtSize <= 0 {
		flushAtSize = 500
	}
	return &Writer{
		store:       s,
		in:          make(chan model.LogEntry, chanCapacity),
		flushEvery:  flushEvery,
		flushAtSize: flushAtSize,
		done:        make(chan struct{}),
	}
}

// Enqueue attempts a non-blocking send onto the writer's channel. It returns
// false if the channel is full, signaling the caller (the ingest HTTP
// handler) to apply backpressure.
func (w *Writer) Enqueue(e model.LogEntry) bool {
	select {
	case w.in <- e:
		return true
	default:
		return false
	}
}

// Run drains the channel until Stop is called, batching entries into
// transactions on a size/time trigger. Intended to run in its own goroutine.
func (w *Writer) Run() {
	ticker := time.NewTicker(w.flushEvery)
	defer ticker.Stop()

	batch := make([]model.LogEntry, 0, w.flushAtSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.store.insertBatch(batch); err != nil {
			log.Printf("store: insert batch failed (dropping %d entries): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e, ok := <-w.in:
			if !ok {
				flush()
				close(w.done)
				return
			}
			batch = append(batch, e)
			if len(batch) >= w.flushAtSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Stop closes the input channel and blocks until the final flush completes.
func (w *Writer) Stop() {
	close(w.in)
	<-w.done
}

// insertBatch groups entries by day partition, ensures each partition
// exists, and inserts each group in one (or a few, if over the parameter
// limit) multi-row transaction.
func (s *Store) insertBatch(entries []model.LogEntry) error {
	byDay := make(map[string][]model.LogEntry)
	for _, e := range entries {
		d := dayKey(time.UnixMicro(e.TS))
		byDay[d] = append(byDay[d], e)
	}

	for day, group := range byDay {
		if err := validateDay(day); err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if err := ensurePartition(tx, day); err != nil {
			tx.Rollback()
			return err
		}
		table := tableName(day)
		for start := 0; start < len(group); start += maxRowsPerStatement {
			end := start + maxRowsPerStatement
			if end > len(group) {
				end = len(group)
			}
			if err := insertRows(tx, table, group[start:end]); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func insertRows(tx *sql.Tx, table string, rows []model.LogEntry) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "INSERT INTO %s (ts, namespace, pod, pod_uid, container, node, stream, trace_id, is_json, raw) VALUES ", table)
	args := make([]any, 0, len(rows)*colsPerRow)
	for i, r := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		isJSON := 0
		if r.IsJSON {
			isJSON = 1
		}
		args = append(args, r.TS, r.Namespace, r.Pod, r.PodUID, r.Container, r.Node, r.Stream, r.TraceID, isJSON, r.Raw)
	}
	_, err := tx.Exec(sb.String(), args...)
	return err
}
