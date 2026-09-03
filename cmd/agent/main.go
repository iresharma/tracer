// Command agent is the tracer DaemonSet log-collection agent: it tails
// container log files on the node, extracts trace correlation data from
// structured JSON app logs, and forwards batches to the central collector.
// It never talks to the Kubernetes API — all metadata comes from the
// kubelet's on-disk log path naming.
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

	log.Printf("agent: starting on node=%s, watching %s, forwarding to %s", cfg.NodeName, cfg.LogRoot, cfg.CollectorURL)
	if err := a.Run(ctx); err != nil {
		log.Fatalf("agent: %v", err)
	}
}
