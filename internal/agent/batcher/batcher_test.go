package batcher

import (
	"sync"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/model"
)

func TestFlushesAtMaxBatchSize(t *testing.T) {
	in := make(chan model.LogEntry, 10)
	var mu sync.Mutex
	var flushes [][]model.LogEntry

	b := New(in, Options{MaxBatchSize: 2, FlushInterval: time.Hour}, func(batch []model.LogEntry) {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]model.LogEntry, len(batch))
		copy(cp, batch)
		flushes = append(flushes, cp)
	})
	stop := make(chan struct{})
	go b.Run(stop)

	in <- model.LogEntry{Raw: "a"}
	in <- model.LogEntry{Raw: "b"}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushes) != 1 || len(flushes[0]) != 2 {
		t.Fatalf("expected one flush of 2 entries, got %v", flushes)
	}
}

func TestFlushesOnInterval(t *testing.T) {
	in := make(chan model.LogEntry, 10)
	var mu sync.Mutex
	var flushes [][]model.LogEntry

	b := New(in, Options{MaxBatchSize: 500, FlushInterval: 50 * time.Millisecond}, func(batch []model.LogEntry) {
		mu.Lock()
		defer mu.Unlock()
		flushes = append(flushes, batch)
	})
	stop := make(chan struct{})
	go b.Run(stop)

	in <- model.LogEntry{Raw: "a"}
	time.Sleep(150 * time.Millisecond)
	close(stop)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushes) == 0 {
		t.Fatalf("expected at least one interval-triggered flush")
	}
}

func TestFlushesOnCloseOfInputChannel(t *testing.T) {
	in := make(chan model.LogEntry, 10)
	var mu sync.Mutex
	flushed := false

	b := New(in, Options{MaxBatchSize: 500, FlushInterval: time.Hour}, func(batch []model.LogEntry) {
		mu.Lock()
		defer mu.Unlock()
		flushed = true
	})

	done := make(chan struct{})
	go func() {
		b.Run(make(chan struct{}))
		close(done)
	}()

	in <- model.LogEntry{Raw: "a"}
	close(in)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after input channel closed")
	}

	mu.Lock()
	defer mu.Unlock()
	if !flushed {
		t.Fatal("expected final flush on channel close")
	}
}
