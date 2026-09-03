package checkpoint

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileStartsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := s.Get("/some/path"); ok {
		t.Fatalf("expected no entry for a fresh store")
	}
}

func TestSetFlushLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoints.json")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Set("/var/log/pods/ns_pod_uid/app/0.log", 12345, 999)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	s2, err := Load(path)
	if err != nil {
		t.Fatalf("Load (reload): %v", err)
	}
	entry, ok := s2.Get("/var/log/pods/ns_pod_uid/app/0.log")
	if !ok {
		t.Fatalf("expected entry to survive reload")
	}
	if entry.Inode != 12345 || entry.Offset != 999 {
		t.Errorf("got %+v, want inode=12345 offset=999", entry)
	}
}

func TestRemove(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "checkpoints.json"))
	s.Set("/a", 1, 1)
	s.Remove("/a")
	if _, ok := s.Get("/a"); ok {
		t.Fatalf("expected entry to be removed")
	}
}
