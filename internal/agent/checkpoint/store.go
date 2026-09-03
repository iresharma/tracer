// Package checkpoint persists per-file read offsets to disk so the agent
// can resume tailing after a restart without re-reading whole files or
// losing its place. Analogous to Filebeat's registry file.
package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/iresharma/tracer/internal/agent/metrics"
)

type Entry struct {
	Inode     uint64    `json:"inode"`
	Offset    int64     `json:"offset"`
	UpdatedAt time.Time `json:"updated_at"`
}

type registry struct {
	Files map[string]Entry `json:"files"`
}

// Store is a thread-safe, periodically-flushed on-disk offset registry.
type Store struct {
	path string
	mu   sync.Mutex
	reg  registry
}

// Load reads the registry at path, if it exists, or starts with an empty
// one otherwise (e.g. the agent's first-ever run on this node).
func Load(path string) (*Store, error) {
	s := &Store{path: path, reg: registry{Files: map[string]Entry{}}}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.reg); err != nil {
		return nil, err
	}
	if s.reg.Files == nil {
		s.reg.Files = map[string]Entry{}
	}
	return s, nil
}

// Get returns the checkpoint for path, if any.
func (s *Store) Get(path string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.reg.Files[path]
	return e, ok
}

// Set records the current offset/inode for path.
func (s *Store) Set(path string, inode uint64, offset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reg.Files[path] = Entry{Inode: inode, Offset: offset, UpdatedAt: time.Now().UTC()}
}

// Remove drops the checkpoint for path (e.g. the file was deleted/rotated
// away and fully drained).
func (s *Store) Remove(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.reg.Files, path)
}

// Flush atomically persists the registry to disk (write to a temp file in
// the same directory, then rename — crash-safe, no torn writes).
func (s *Store) Flush() error {
	timer := prometheus.NewTimer(metrics.CheckpointFlushDuration)
	defer func() {
		timer.ObserveDuration()
		metrics.CheckpointFlushesTotal.Inc()
	}()

	s.mu.Lock()
	data, err := json.Marshal(s.reg)
	s.mu.Unlock()
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".checkpoints-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// RunPeriodicFlush blocks flushing the registry every interval until done
// is closed, then performs one final flush.
func (s *Store) RunPeriodicFlush(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = s.Flush()
		case <-done:
			_ = s.Flush()
			return
		}
	}
}
