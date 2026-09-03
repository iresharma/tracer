package tailer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/agent/checkpoint"
)

func newTestTailer(t *testing.T, path string, cp *checkpoint.Store) (*Tailer, chan Line) {
	t.Helper()
	out := make(chan Line, 100)
	tl := New(path, Options{PollInterval: time.Hour, StartAtEnd: false}, cp, out)
	return tl, out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func drain(t *testing.T, out chan Line) []string {
	t.Helper()
	var lines []string
	for {
		select {
		case l := <-out:
			lines = append(lines, l.Content)
		default:
			return lines
		}
	}
}

func criLine(content string) string {
	return time.Now().UTC().Format(time.RFC3339Nano) + " stdout F " + content + "\n"
}

func TestTailerReadsExistingAndNewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0.log")
	writeFile(t, path, criLine("line one")+criLine("line two"))

	cp, _ := checkpoint.Load(filepath.Join(dir, "checkpoints.json"))
	tl, out := newTestTailer(t, path, cp)

	if err := tl.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := tl.readAvailable(); err != nil {
		t.Fatalf("readAvailable: %v", err)
	}
	got := drain(t, out)
	if len(got) != 2 || got[0] != "line one" || got[1] != "line two" {
		t.Fatalf("unexpected lines: %v", got)
	}

	appendFile(t, path, criLine("line three"))
	if err := tl.poll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	got = drain(t, out)
	if len(got) != 1 || got[0] != "line three" {
		t.Fatalf("unexpected lines after append: %v", got)
	}
}

func TestTailerResumesFromCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0.log")
	cpPath := filepath.Join(dir, "checkpoints.json")
	writeFile(t, path, criLine("line one")+criLine("line two"))

	cp, _ := checkpoint.Load(cpPath)
	tl, out := newTestTailer(t, path, cp)
	if err := tl.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := tl.readAvailable(); err != nil {
		t.Fatalf("readAvailable: %v", err)
	}
	drain(t, out) // consume both lines
	if err := cp.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	tl.file.Close()

	// Simulate agent restart: fresh checkpoint store loaded from disk,
	// fresh tailer instance, more data appended while "down".
	appendFile(t, path, criLine("line three"))
	cp2, err := checkpoint.Load(cpPath)
	if err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	tl2, out2 := newTestTailer(t, path, cp2)
	if err := tl2.open(); err != nil {
		t.Fatalf("open (resume): %v", err)
	}
	if err := tl2.readAvailable(); err != nil {
		t.Fatalf("readAvailable (resume): %v", err)
	}
	got := drain(t, out2)
	if len(got) != 1 || got[0] != "line three" {
		t.Fatalf("expected only the new line on resume, got %v", got)
	}
}

func TestTailerHandlesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0.log")
	cp, _ := checkpoint.Load(filepath.Join(dir, "checkpoints.json"))
	writeFile(t, path, criLine("old file line"))

	tl, out := newTestTailer(t, path, cp)
	if err := tl.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := tl.readAvailable(); err != nil {
		t.Fatalf("readAvailable: %v", err)
	}
	drain(t, out)

	// Simulate kubelet rotation: rename the old file away, create a fresh
	// one at the same path (different inode).
	if err := os.Rename(path, filepath.Join(dir, "0.log.rotated")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	writeFile(t, path, criLine("new file line"))

	if err := tl.poll(); err != nil {
		t.Fatalf("poll (rotation): %v", err)
	}
	got := drain(t, out)
	if len(got) != 1 || got[0] != "new file line" {
		t.Fatalf("expected only the new file's line after rotation, got %v", got)
	}
}

func TestTailerHandlesTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0.log")
	cp, _ := checkpoint.Load(filepath.Join(dir, "checkpoints.json"))
	writeFile(t, path, criLine("this is a long line that will be truncated"))

	tl, out := newTestTailer(t, path, cp)
	if err := tl.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := tl.readAvailable(); err != nil {
		t.Fatalf("readAvailable: %v", err)
	}
	drain(t, out)

	// Truncate in place (same inode, smaller size) and write a shorter line.
	writeFile(t, path, criLine("short"))

	if err := tl.poll(); err != nil {
		t.Fatalf("poll (truncation): %v", err)
	}
	got := drain(t, out)
	if len(got) != 1 || got[0] != "short" {
		t.Fatalf("expected only the post-truncation line, got %v", got)
	}
}

func TestTailerRewindsOnPartialTrailingLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0.log")
	cp, _ := checkpoint.Load(filepath.Join(dir, "checkpoints.json"))

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	// Write a complete line followed by a partial one with no trailing
	// newline, simulating a poll racing an in-progress write.
	writeFile(t, path, criLine("complete line")+ts+" stdout F unfinished-wri")

	tl, out := newTestTailer(t, path, cp)
	if err := tl.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := tl.readAvailable(); err != nil {
		t.Fatalf("readAvailable: %v", err)
	}
	got := drain(t, out)
	if len(got) != 1 || got[0] != "complete line" {
		t.Fatalf("expected only the complete line, got %v", got)
	}

	// Writer finishes the line.
	appendFile(t, path, "te\n")
	if err := tl.poll(); err != nil {
		t.Fatalf("poll: %v", err)
	}
	got = drain(t, out)
	if len(got) != 1 || got[0] != "unfinished-write" {
		t.Fatalf("expected the completed line with no duplication, got %v", got)
	}
}
