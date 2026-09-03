// Command agent is the tracer log-collection agent: it extracts trace
// correlation data from structured JSON app logs and forwards batches to
// the central collector. Two ingestion modes (see internal/agent/config):
// "hostpath" (default) tails container log files on the node directly,
// with no Kubernetes API access at all; "api" streams logs via the
// Kubernetes pods/log API instead, for namespaces that can't grant
// hostPath.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/iresharma/tracer/internal/agent"
	"github.com/iresharma/tracer/internal/agent/config"
)

func main() {
	cfg := config.Load()

	a, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("agent: init failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if cfg.IngestionMode == "api" {
		log.Printf("agent: starting in api mode, forwarding to %s", cfg.CollectorURL)
	} else {
		log.Printf("agent: starting on node=%s, watching %s, forwarding to %s", cfg.NodeName, cfg.LogRoot, cfg.CollectorURL)
	}
	if err := a.Run(ctx); err != nil {
		log.Fatalf("agent: %v", err)
	}
}
