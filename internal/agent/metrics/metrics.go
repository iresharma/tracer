// Package metrics defines the agent's Prometheus metrics as package-level
// collectors, registered once at init via promauto. Other agent packages
// import this one and call into these vars directly at the point an event
// happens — no dependency injection needed, matching the standard
// client_golang pattern of a global default registry.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "tracer_agent"

var (
	// Tailer (internal/agent/tailer)
	TailerLinesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "tailer",
		Name:      "lines_total",
		Help:      "Total number of complete log lines read across all tailed files.",
	})
	TailerBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "tailer",
		Name:      "bytes_total",
		Help:      "Total number of raw bytes read across all tailed files.",
	})
	TailerRotationsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "tailer",
		Name:      "rotations_total",
		Help:      "Total number of log rotations (inode change or in-place truncation) handled.",
	})
	TailerMalformedLinesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "tailer",
		Name:      "malformed_lines_total",
		Help:      "Total number of lines skipped because they didn't parse as CRI or Docker json-file format.",
	})

	// Batcher (internal/agent/batcher)
	BatcherBatchesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "batcher",
		Name:      "batches_total",
		Help:      "Total number of batches flushed to the forwarder.",
	})
	BatcherEntriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "batcher",
		Name:      "entries_total",
		Help:      "Total number of log entries batched (divide by batches_total for average batch size).",
	})

	// Forwarder (internal/agent/forwarder)
	ForwarderSendTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "forwarder",
		Name:      "send_total",
		Help:      "Total send attempts to the collector, by outcome.",
	}, []string{"result"}) // "success" | "failure"
	// RingDropped is the single most important health signal this agent
	// exposes: a nonzero rate means the ring buffer is full and old,
	// unsent batches are being discarded — the collector has been
	// unreachable long enough to actually lose data.
	ForwarderRingDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "forwarder",
		Name:      "ring_dropped_total",
		Help:      "Total batches dropped from the retry ring because it was full (data loss).",
	})
	ForwarderRingDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "forwarder",
		Name:      "ring_depth",
		Help:      "Current number of unsent batches queued in the retry ring.",
	})
	ForwarderBackoffSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "forwarder",
		Name:      "backoff_seconds",
		Help:      "Current retry backoff duration after the most recent send failure.",
	})

	// Discovery (internal/agent/discovery)
	DiscoveryFilesTracked = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "discovery",
		Name:      "files_tracked",
		Help:      "Total distinct log files discovered since the agent started. Monotonically non-decreasing — the discovery set is never pruned when a file disappears, so this tracks cumulative pod/container churn on the node, not live tailer count.",
	})

	// Checkpoint (internal/agent/checkpoint)
	CheckpointFlushesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "checkpoint",
		Name:      "flushes_total",
		Help:      "Total number of checkpoint registry flushes to disk.",
	})
	CheckpointFlushDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "checkpoint",
		Name:      "flush_duration_seconds",
		Help:      "Duration of each checkpoint registry flush.",
		Buckets:   []float64{.0005, .001, .005, .01, .05, .1, .5, 1},
	})

	// Podwatcher (internal/agent/podwatcher) — the "api" ingestion mode,
	// an alternative to the tailer/discovery pair above for namespaces
	// that can't grant hostPath (see the package doc comment for why).
	PodwatcherStreamsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "podwatcher",
		Name:      "streams_active",
		Help:      "Current number of container log streams being followed.",
	})
	PodwatcherLinesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "podwatcher",
		Name:      "lines_total",
		Help:      "Total number of log lines received across all streamed containers.",
	})
	PodwatcherStreamReconnectsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "podwatcher",
		Name:      "stream_reconnects_total",
		Help:      "Total number of times a container's log stream ended and was reopened (e.g. container restart).",
	})
	PodwatcherWatchReconnectsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "podwatcher",
		Name:      "watch_reconnects_total",
		Help:      "Total number of times the pod watch connection itself ended and was reopened (via a fresh LIST+WATCH).",
	})
)
