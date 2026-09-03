package discovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanOnceFindsExistingLogFiles(t *testing.T) {
	root := t.TempDir()
	podDir := filepath.Join(root, "default_app-1_uid1", "app")
	if err := os.MkdirAll(podDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(podDir, "0.log")
	if err := os.WriteFile(logPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A non-.log file should be ignored.
	if err := os.WriteFile(filepath.Join(podDir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	found := make(chan string, 10)
	s := New(root, time.Hour, found)
	s.scanOnce()

	select {
	case p := <-found:
		if p != logPath {
			t.Fatalf("got %q, want %q", p, logPath)
		}
	default:
		t.Fatalf("expected to find %q", logPath)
	}

	select {
	case p := <-found:
		t.Fatalf("expected no further finds, got %q", p)
	default:
	}

	// A second scan should not re-report the same file.
	s.scanOnce()
	select {
	case p := <-found:
		t.Fatalf("expected no duplicate report, got %q", p)
	default:
	}
}

func TestScanOnceFindsNewFilesAcrossCalls(t *testing.T) {
	root := t.TempDir()
	found := make(chan string, 10)
	s := New(root, time.Hour, found)
	s.scanOnce()

	podDir := filepath.Join(root, "ns_pod_uid2", "container")
	os.MkdirAll(podDir, 0o755)
	logPath := filepath.Join(podDir, "0.log")
	os.WriteFile(logPath, []byte("x\n"), 0o644)

	s.scanOnce()
	select {
	case p := <-found:
		if p != logPath {
			t.Fatalf("got %q, want %q", p, logPath)
		}
	default:
		t.Fatalf("expected to find newly created %q", logPath)
	}
}
