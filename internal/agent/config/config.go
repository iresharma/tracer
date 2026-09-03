// Package config loads agent configuration from the environment, suitable
// for a ConfigMap-driven DaemonSet plus Downward-API-injected NODE_NAME.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	NodeName        string
	CollectorURL    string
	LogRoot         string
	CheckpointPath  string
	TraceIDField    string
	PollInterval    time.Duration
	RescanInterval  time.Duration
	MaxLineBytes    int
	FlushInterval   time.Duration
	MaxBatchSize    int
	MaxBatchBytes   int
	RingCapacity    int
	CheckpointFlush time.Duration
	StartAtEnd      bool
	MetricsAddr     string

	// IngestionMode selects how the agent finds log lines:
	//   "hostpath" (default) — tail node-local CRI log files via a hostPath
	//     mount. Needs no Kubernetes API access, but needs hostPath, which
	//     restricted-tier Pod Security namespaces reject.
	//   "api" — stream logs via the Kubernetes pods/log API (the same
	//     mechanism `kubectl logs -f` uses). No hostPath, works in a
	//     restricted-tier namespace, but needs RBAC (get/list/watch pods,
	//     get pods/log) and only sees pods in WatchNamespace.
	IngestionMode string
	// WatchNamespace is the namespace to watch in "api" mode. Empty means
	// "the agent's own namespace", read from the in-cluster serviceaccount
	// namespace file at startup.
	WatchNamespace string
}

func Load() Config {
	return Config{
		NodeName:        getEnv("NODE_NAME", "unknown-node"),
		CollectorURL:    getEnv("COLLECTOR_URL", "http://tracer-collector:8080"),
		LogRoot:         getEnv("LOG_ROOT", "/var/log/pods"),
		CheckpointPath:  getEnv("CHECKPOINT_PATH", "/var/lib/tracer-agent/checkpoints.json"),
		TraceIDField:    getEnv("TRACE_ID_FIELD", "trace_id"),
		PollInterval:    getEnvDuration("POLL_INTERVAL", 250*time.Millisecond),
		RescanInterval:  getEnvDuration("RESCAN_INTERVAL", 30*time.Second),
		MaxLineBytes:    getEnvInt("MAX_LINE_BYTES", 256*1024),
		FlushInterval:   getEnvDuration("FLUSH_INTERVAL", 2*time.Second),
		MaxBatchSize:    getEnvInt("MAX_BATCH_SIZE", 500),
		MaxBatchBytes:   getEnvInt("MAX_BATCH_BYTES", 512*1024),
		RingCapacity:    getEnvInt("RING_BUFFER_CAPACITY", 20),
		CheckpointFlush: getEnvDuration("CHECKPOINT_FLUSH_INTERVAL", 3*time.Second),
		StartAtEnd:      getEnvBool("START_AT_END", true),
		MetricsAddr:     getEnv("METRICS_ADDR", ":9091"),
		IngestionMode:   getEnv("INGESTION_MODE", "hostpath"),
		WatchNamespace:  getEnv("WATCH_NAMESPACE", ""),
	}
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
