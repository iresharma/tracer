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

	// AuthEnabled gates HTTP Basic Auth on the UI/JSON query API and
	// Bearer-token auth on the ingest endpoint. Off by default so existing
	// deployments aren't broken until the operator opts in.
	AuthEnabled     bool
	AuthUsername    string
	AuthPassword    string
	AuthIngestToken string
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
		AuthEnabled:       getEnvBool("AUTH_ENABLED", false),
		AuthUsername:      getEnv("AUTH_USERNAME", ""),
		AuthPassword:      getEnv("AUTH_PASSWORD", ""),
		AuthIngestToken:   getEnv("AUTH_INGEST_TOKEN", ""),
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

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
