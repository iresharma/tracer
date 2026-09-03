// Package batcher accumulates log entries and flushes them as a batch when
// a size, byte, or time threshold is reached — whichever comes first.
package batcher

import (
	"time"

	"github.com/iresharma/tracer/internal/agent/metrics"
	"github.com/iresharma/tracer/internal/model"
)

type Options struct {
	MaxBatchSize  int
	MaxBatchBytes int
	FlushInterval time.Duration
}

func (o *Options) setDefaults() {
	if o.MaxBatchSize <= 0 {
		o.MaxBatchSize = 500
	}
	if o.MaxBatchBytes <= 0 {
		o.MaxBatchBytes = 512 * 1024
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = 2 * time.Second
	}
}

// Batcher reads entries from in and calls flush with accumulated batches.
type Batcher struct {
	opts  Options
	in    <-chan model.LogEntry
	flush func([]model.LogEntry)
}

func New(in <-chan model.LogEntry, opts Options, flush func([]model.LogEntry)) *Batcher {
	opts.setDefaults()
	return &Batcher{opts: opts, in: in, flush: flush}
}

// Run blocks batching entries until in is closed (a final partial batch is
// flushed before returning), or stop is closed.
func (b *Batcher) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(b.opts.FlushInterval)
	defer ticker.Stop()

	batch := make([]model.LogEntry, 0, b.opts.MaxBatchSize)
	size := 0

	doFlush := func() {
		if len(batch) == 0 {
			return
		}
		metrics.BatcherBatchesTotal.Inc()
		metrics.BatcherEntriesTotal.Add(float64(len(batch)))
		b.flush(batch)
		batch = make([]model.LogEntry, 0, b.opts.MaxBatchSize)
		size = 0
	}

	for {
		select {
		case e, ok := <-b.in:
			if !ok {
				doFlush()
				return
			}
			batch = append(batch, e)
			size += len(e.Raw)
			if len(batch) >= b.opts.MaxBatchSize || size >= b.opts.MaxBatchBytes {
				doFlush()
			}
		case <-ticker.C:
			doFlush()
		case <-stop:
			doFlush()
			return
		}
	}
}
