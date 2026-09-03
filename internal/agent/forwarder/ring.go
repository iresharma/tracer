package forwarder

import (
	"sync"

	"github.com/iresharma/tracer/internal/agent/metrics"
	"github.com/iresharma/tracer/internal/model"
)

// ring is a bounded, thread-safe FIFO of pending batches. When full, Push
// drops the oldest queued batch to make room — the agent must never grow
// unbounded on a node during a sustained collector outage.
type ring struct {
	mu       sync.Mutex
	items    [][]model.LogEntry
	capacity int
	dropped  int
}

func newRing(capacity int) *ring {
	if capacity <= 0 {
		capacity = 20
	}
	return &ring{capacity: capacity}
}

func (r *ring) Push(batch []model.LogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) >= r.capacity {
		r.items = r.items[1:]
		r.dropped++
		metrics.ForwarderRingDroppedTotal.Inc()
	}
	r.items = append(r.items, batch)
	metrics.ForwarderRingDepth.Set(float64(len(r.items)))
}

// Peek returns the oldest batch without removing it, or ok=false if empty.
func (r *ring) Peek() (batch []model.LogEntry, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) == 0 {
		return nil, false
	}
	return r.items[0], true
}

// Pop removes the oldest batch (call after Peek succeeds and the batch was
// forwarded successfully).
func (r *ring) Pop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) == 0 {
		return
	}
	r.items = r.items[1:]
	metrics.ForwarderRingDepth.Set(float64(len(r.items)))
}

func (r *ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

func (r *ring) Dropped() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}
