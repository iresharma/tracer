// Package config loads collector configuration from the environment,
// suitable for a ConfigMap-driven Kubernetes Deployment.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr          string
	DBPath            string
	RetentionDays     int
	TraceWindowDays   int
	SQLiteCacheKB     int
	IngestChanCap     int
	WriterFlushEvery  int // milliseconds
	WriterFlushAtSize int

	// IngestAuthToken guards POST /api/v1/logs (the agent-to-collector,
	// service-to-service path): callers must send
	// "Authorization: Bearer <token>". It is a shared secret the agent's
	// forwarder is configured with separately (see
	// internal/agent/config.Config.IngestAuthToken) — the two sides must
	// agree on the same value. Empty disables the check (unauthenticated),
	// which is the zero-config default so existing deployments and tests
	// keep working; a startup warning is logged when it's empty.
	IngestAuthToken string
	// UIAuthUsername and UIAuthPassword guard the server-rendered UI
	// (internal/collector/ui) and JSON query API
	// (internal/collector/queryapi) via HTTP Basic auth. Both must be set
	// to enable the check; if either is empty, that surface runs
	// unauthenticated (same zero-config default reasoning as
	// IngestAuthToken).
	UIAuthUsername string
	UIAuthPassword string
}

func Load() Config {
	return Config{
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		DBPath:            getEnv("DB_PATH", "/data/tracer.db"),
		RetentionDays:     getEnvInt("RETENTION_DAYS", 10),
		TraceWindowDays:   getEnvInt("TRACE_SEARCH_WINDOW_DAYS", 2),
		SQLiteCacheKB:     getEnvInt("SQLITE_CACHE_KB", 16000),
		IngestChanCap:     getEnvInt("INGEST_CHAN_CAPACITY", 5000),
		WriterFlushEvery:  getEnvInt("WRITER_FLUSH_MS", 200),
		WriterFlushAtSize: getEnvInt("WRITER_FLUSH_SIZE", 500),
		IngestAuthToken:   getEnv("INGEST_AUTH_TOKEN", ""),
		UIAuthUsername:    getEnv("UI_AUTH_USERNAME", ""),
		UIAuthPassword:    getEnv("UI_AUTH_PASSWORD", ""),
	}
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
