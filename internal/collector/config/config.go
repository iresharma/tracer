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

	// BasicAuthUser/BasicAuthPass gate the UI and query API behind HTTP
	// Basic Auth. IngestAuthToken gates the ingest endpoint behind a
	// bearer token. Each is disabled (pass-through) when left empty, so
	// existing deploys without the auth Secret don't break at startup —
	// operators SHOULD set these in production (see README).
	BasicAuthUser   string
	BasicAuthPass   string
	IngestAuthToken string
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
		BasicAuthUser:     getEnv("AUTH_USERNAME", ""),
		BasicAuthPass:     getEnv("AUTH_PASSWORD", ""),
		IngestAuthToken:   getEnv("INGEST_AUTH_TOKEN", ""),
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
