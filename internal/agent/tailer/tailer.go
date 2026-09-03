// Package tailer implements per-file, poll-based tailing of container log
// files with kubelet-rotation detection and checkpoint-based resume.
//
// Polling (not per-file fsnotify) is deliberate: per-file watches don't
// scale well against fd/watch-descriptor pressure on a node with many
// containers, and don't reliably survive the kubelet's rename-and-recreate
// rotation pattern. A cheap sleep-poll loop is simple, memory-bounded (one
// small bufio.Reader per file), and CPU-idle between polls.
package tailer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/iresharma/tracer/internal/agent/checkpoint"
	"github.com/iresharma/tracer/internal/agent/parser"
)

// Line is one reassembled, complete container log line, tagged with the
// source file path so the caller can derive namespace/pod/container.
type Line struct {
	Path string
	parser.CRILine
}

type Options struct {
	PollInterval time.Duration
	MaxLineBytes int
	// StartAtEnd controls where a file with no existing checkpoint begins:
	// true (default) avoids replaying a node's entire log history on first
	// agent start; false backfills from the beginning.
	StartAtEnd bool
}

func (o *Options) setDefaults() {
	if o.PollInterval <= 0 {
		o.PollInterval = 250 * time.Millisecond
	}
	if o.MaxLineBytes <= 0 {
		o.MaxLineBytes = 256 * 1024
	}
}

// Tailer follows a single file, emitting complete lines onto out.
type Tailer struct {
	path string
	opts Options
	cp   *checkpoint.Store
	out  chan<- Line

	file   *os.File
	reader *bufio.Reader
	inode  uint64
	offset int64
	reasm  parser.Reassembler
}

func New(path string, opts Options, cp *checkpoint.Store, out chan<- Line) *Tailer {
	opts.setDefaults()
	return &Tailer{path: path, opts: opts, cp: cp, out: out}
}

// Run opens the file (resuming from any checkpoint) and polls it until ctx
// is cancelled or the file disappears permanently.
func (t *Tailer) Run(ctx context.Context) error {
	if err := t.open(); err != nil {
		return err
	}
	defer func() {
		if t.file != nil {
			t.file.Close()
		}
	}()

	ticker := time.NewTicker(t.opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := t.poll(); err != nil {
				if os.IsNotExist(err) {
					// File was removed (container gone, log GC'd) and never
					// came back; stop tailing it.
					t.cp.Remove(t.path)
					return nil
				}
				log.Printf("tailer: %s: %v", t.path, err)
			}
		}
	}
}

func (t *Tailer) open() error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	inode, err := inodeOf(fi)
	if err != nil {
		f.Close()
		return err
	}

	var startOffset int64
	if entry, ok := t.cp.Get(t.path); ok && entry.Inode == inode {
		startOffset = entry.Offset
	} else if t.opts.StartAtEnd {
		startOffset = fi.Size()
	}
	if startOffset > fi.Size() {
		// File shrank since the checkpoint was written (truncated); restart.
		startOffset = 0
	}
	if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
		f.Close()
		return err
	}

	t.file = f
	t.reader = bufio.NewReaderSize(f, 64*1024)
	t.inode = inode
	t.offset = startOffset
	t.cp.Set(t.path, inode, startOffset)
	return nil
}

// poll checks for rotation/truncation, then reads whatever complete lines
// are currently available.
func (t *Tailer) poll() error {
	fi, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	currentInode, err := inodeOf(fi)
	if err != nil {
		return err
	}

	if currentInode != t.inode {
		if err := t.handleRotation(); err != nil {
			return fmt.Errorf("handle rotation: %w", err)
		}
		return nil
	}
	if fi.Size() < t.offset {
		// Truncated in place without a rename (some runtimes/log rotators
		// do this). Reopen from the start.
		if err := t.reopenFromStart(); err != nil {
			return err
		}
	}

	return t.readAvailable()
}

func (t *Tailer) handleRotation() error {
	// Drain whatever remains in the old (rotated-away) file first, then
	// switch to the new file at the same path, starting at offset 0, and
	// pick up anything already written to it.
	if err := t.readAvailable(); err != nil {
		log.Printf("tailer: %s: error draining rotated file: %v", t.path, err)
	}
	if err := t.reopenFromStart(); err != nil {
		return err
	}
	return t.readAvailable()
}

func (t *Tailer) reopenFromStart() error {
	t.file.Close()
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	inode, err := inodeOf(fi)
	if err != nil {
		f.Close()
		return err
	}
	t.file = f
	t.reader = bufio.NewReaderSize(f, 64*1024)
	t.inode = inode
	t.offset = 0
	t.reasm = parser.Reassembler{}
	t.cp.Set(t.path, inode, 0)
	return nil
}

// readAvailable reads whole lines currently sitting in the file, feeding
// each through the CRI reassembler and emitting completed logical lines.
// If the final read hits EOF mid-line (the writer hasn't finished this
// line yet), it rewinds to the last confirmed offset so the same bytes are
// re-read whole on the next poll — this never loses or duplicates data.
func (t *Tailer) readAvailable() error {
	for {
		raw, err := t.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if raw != "" {
					// Incomplete trailing line: rewind and retry next poll.
					if _, serr := t.file.Seek(t.offset, io.SeekStart); serr != nil {
						return serr
					}
					t.reader.Reset(t.file)
				}
				return nil
			}
			return err
		}

		t.offset += int64(len(raw))
		if len(raw) > t.opts.MaxLineBytes {
			raw = raw[len(raw)-t.opts.MaxLineBytes:]
		}

		line, ready, ferr := t.reasm.Feed(raw)
		if ferr != nil {
			// Malformed line; skip it and keep tailing rather than
			// aborting the whole file.
			continue
		}
		if !ready {
			continue
		}

		select {
		case t.out <- Line{Path: t.path, CRILine: line}:
		default:
			// Downstream (batcher) is momentarily full; drop this line
			// rather than block the tailer indefinitely. The batcher/
			// forwarder layer owns bounded buffering with its own
			// drop-oldest policy for sustained backpressure.
			log.Printf("tailer: %s: downstream full, dropping line", t.path)
		}

		t.cp.Set(t.path, t.inode, t.offset)
	}
}
