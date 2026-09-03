// Package forwarder ships batches of log entries to the collector over
// HTTP, gzip-compressed, with bounded retry buffering so a collector
// outage degrades gracefully (oldest-batch drop) instead of growing the
// agent's memory without bound.
package forwarder

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/iresharma/tracer/internal/agent/metrics"
	"github.com/iresharma/tracer/internal/model"
)

type Options struct {
	CollectorURL string
	AgentID      string
	Timeout      time.Duration
	MinBackoff   time.Duration
	MaxBackoff   time.Duration
	RingCapacity int
}

func (o *Options) setDefaults() {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 30 * time.Second
	}
	if o.RingCapacity <= 0 {
		o.RingCapacity = 20
	}
}

type Forwarder struct {
	opts    Options
	client  *http.Client
	ring    *ring
	backoff time.Duration
}

func New(opts Options) *Forwarder {
	opts.setDefaults()
	return &Forwarder{
		opts:    opts,
		client:  &http.Client{Timeout: opts.Timeout},
		ring:    newRing(opts.RingCapacity),
		backoff: opts.MinBackoff,
	}
}

// Submit enqueues a batch built by the batcher. Non-blocking: applies the
// ring's drop-oldest policy if the agent is backed up.
func (f *Forwarder) Submit(batch []model.LogEntry) {
	if len(batch) == 0 {
		return
	}
	f.ring.Push(batch)
}

// Run drains the ring, POSTing batches to the collector, until stop is
// closed. On failure it retries the same batch with exponential backoff
// (capped at MaxBackoff) rather than dropping it immediately — the ring's
// own capacity limit is what bounds total memory during a sustained outage.
func (f *Forwarder) Run(stop <-chan struct{}) {
	idleWait := time.NewTicker(200 * time.Millisecond)
	defer idleWait.Stop()

	for {
		batch, ok := f.ring.Peek()
		if !ok {
			select {
			case <-stop:
				return
			case <-idleWait.C:
				continue
			}
		}

		if err := f.send(batch); err != nil {
			metrics.ForwarderSendTotal.WithLabelValues("failure").Inc()
			log.Printf("forwarder: send failed (%d entries): %v; retrying in %s", len(batch), err, f.backoff)
			select {
			case <-stop:
				return
			case <-time.After(f.backoff):
			}
			f.backoff *= 2
			if f.backoff > f.opts.MaxBackoff {
				f.backoff = f.opts.MaxBackoff
			}
			metrics.ForwarderBackoffSeconds.Set(f.backoff.Seconds())
			continue
		}

		metrics.ForwarderSendTotal.WithLabelValues("success").Inc()
		f.ring.Pop()
		f.backoff = f.opts.MinBackoff
		metrics.ForwarderBackoffSeconds.Set(f.backoff.Seconds())

		select {
		case <-stop:
			return
		default:
		}
	}
}

func (f *Forwarder) send(entries []model.LogEntry) error {
	batch := model.Batch{AgentID: f.opts.AgentID, Entries: entries}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gz).Encode(batch); err != nil {
		gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.opts.CollectorURL+"/api/v1/logs", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{Code: resp.StatusCode}
	}
	return nil
}

type httpStatusError struct{ Code int }

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status %d %s", e.Code, http.StatusText(e.Code))
}
