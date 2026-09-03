// Command collector runs the tracer central collector: log ingestion,
// SQLite-backed storage with day-partitioned retention, a JSON query API,
// and the server-rendered HTMX UI, all in a single process.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/iresharma/tracer/internal/collector"
	"github.com/iresharma/tracer/internal/collector/config"
)

func main() {
	cfg := config.Load()

	srv, err := collector.New(cfg)
	if err != nil {
		log.Fatalf("collector: init failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("collector: %v", err)
	}
}
