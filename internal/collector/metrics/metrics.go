// Package metrics defines the collector's Prometheus metrics as
// package-level collectors, registered once at init via promauto. Other
// collector packages import this one and call into these vars directly at
// the point an event happens — no dependency injection needed, matching
// the standard client_golang pattern of a global default registry.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "tracer_collector"

var (
	// Ingest (internal/collector/ingest)
	IngestBatchesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "ingest",
		Name:      "batches_total",
		Help:      "Total number of log batches received via POST /api/v1/logs.",
	})
	IngestEntriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "ingest",
		Name:      "entries_total",
		Help:      "Total number of individual log entries received, by outcome.",
	}, []string{"result"}) // "accepted" | "rejected" | "queue_full"

	// Writer (internal/collector/store)
	WriterQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "writer",
		Name:      "queue_depth",
		Help:      "Current number of entries buffered in the writer's input channel.",
	})
	WriterQueueCapacity = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "writer",
		Name:      "queue_capacity",
		Help:      "Capacity of the writer's input channel.",
	})
	WriterFlushesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "writer",
		Name:      "flushes_total",
		Help:      "Total number of batch flushes to SQLite.",
	})
	WriterFlushDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "writer",
		Name:      "flush_duration_seconds",
		Help:      "Duration of each flush transaction (partition ensure + inserts + commit).",
		Buckets:   prometheus.DefBuckets,
	})
	WriterRowsWrittenTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "writer",
		Name:      "rows_written_total",
		Help:      "Total number of log rows successfully written to SQLite.",
	})
	WriterInsertErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "writer",
		Name:      "insert_errors_total",
		Help:      "Total number of failed flush attempts (the whole batch is dropped on error).",
	})

	// Retention (internal/collector, retentionLoop/prune)
	RetentionPartitionsDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "retention",
		Name:      "partitions_dropped_total",
		Help:      "Total number of day-partitions dropped by the retention prune loop.",
	})
	RetentionPruneDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "retention",
		Name:      "prune_duration_seconds",
		Help:      "Duration of each retention prune pass.",
		Buckets:   []float64{.01, .05, .1, .5, 1, 5, 10, 30},
	})

	// HTTP (all handlers, via middleware in collector.go)
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests, by route and status code.",
	}, []string{"route", "code"})
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration, by route.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route"})

	// Ad-hoc SQL query page (internal/collector/store/rawquery.go)
	QueryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "query",
		Name:      "total",
		Help:      "Total ad-hoc SQL queries run from the Query page, by outcome.",
	}, []string{"result"}) // "ok" | "rejected" | "error"
	QueryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "query",
		Name:      "duration_seconds",
		Help:      "Duration of ad-hoc SQL queries that passed validation.",
		Buckets:   prometheus.DefBuckets,
	})
)
