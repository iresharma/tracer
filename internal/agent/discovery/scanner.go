// Package discovery finds container log files under a kubelet pod-logs
// root and notifies callers of new ones. It combines an fsnotify watch
// (for near-instant pickup of new pods) with a periodic full rescan, since
// fsnotify events can be coalesced or dropped under high container churn.
package discovery

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Scanner watches root (e.g. /var/log/pods) for container log files and
// reports newly discovered ones on Found. It never reports the same path
// twice.
type Scanner struct {
	Root        string
	RescanEvery time.Duration
	Found       chan<- string

	seen map[string]struct{}
}

func New(root string, rescanEvery time.Duration, found chan<- string) *Scanner {
	if rescanEvery <= 0 {
		rescanEvery = 30 * time.Second
	}
	return &Scanner{Root: root, RescanEvery: rescanEvery, Found: found, seen: map[string]struct{}{}}
}

// Run blocks scanning until stop is closed.
func (s *Scanner) Run(stop <-chan struct{}) {
	s.scanOnce()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("discovery: fsnotify unavailable (%v), falling back to polling only", err)
		s.pollOnly(stop)
		return
	}
	defer watcher.Close()

	s.addWatchesRecursive(watcher, s.Root)

	ticker := time.NewTicker(s.RescanEvery)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create) != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					s.addWatchesRecursive(watcher, ev.Name)
				}
				s.scanOnce()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("discovery: watcher error: %v", err)
		case <-ticker.C:
			s.scanOnce()
		}
	}
}

func (s *Scanner) pollOnly(stop <-chan struct{}) {
	ticker := time.NewTicker(s.RescanEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.scanOnce()
		}
	}
}

func (s *Scanner) addWatchesRecursive(w *fsnotify.Watcher, dir string) {
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		_ = w.Add(path)
		return nil
	})
}

// scanOnce walks the whole tree and reports any not-yet-seen *.log files.
func (s *Scanner) scanOnce() {
	filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".log") {
			return nil
		}
		if _, ok := s.seen[path]; ok {
			return nil
		}
		s.seen[path] = struct{}{}
		select {
		case s.Found <- path:
		default:
			log.Printf("discovery: found-channel full, will retry %s next scan", path)
			delete(s.seen, path)
		}
		return nil
	})
}
