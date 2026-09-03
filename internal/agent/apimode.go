package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/iresharma/tracer/internal/agent/parser"
	"github.com/iresharma/tracer/internal/agent/podwatcher"
	"github.com/iresharma/tracer/internal/model"
)

// runAPIMode is the "api" IngestionMode pipeline: watches Pods via the
// Kubernetes API and streams their logs via pods/log, instead of tailing
// hostPath files (see internal/agent/podwatcher's package doc for why a
// namespace might need this — no hostPath, so it passes restricted-tier
// Pod Security Standards, at the cost of needing RBAC and only seeing one
// namespace).
func (a *Agent) runAPIMode(ctx context.Context, out chan<- model.LogEntry, stop <-chan struct{}) error {
	cfg, err := podwatcher.LoadInClusterConfig()
	if err != nil {
		return fmt.Errorf("api mode: %w", err)
	}
	client, err := cfg.HTTPClient()
	if err != nil {
		return fmt.Errorf("api mode: %w", err)
	}

	ns := a.cfg.WatchNamespace
	if ns == "" {
		ns = cfg.DefaultNamespace
	}

	lines := make(chan podwatcher.Line, 1000)
	watcher := podwatcher.New(cfg, client, ns, lines)

	go func() {
		if err := watcher.Run(ctx); err != nil {
			log.Printf("agent: podwatcher exited: %v", err)
		}
	}()

	log.Printf("agent: api mode watching namespace %q via %s", ns, cfg.BaseURL)

	for {
		select {
		case <-stop:
			return nil
		case l, ok := <-lines:
			if !ok {
				return nil
			}
			isJSON, traceID := parser.ExtractTraceID(l.Content, a.cfg.TraceIDField)
			entry := model.LogEntry{
				TS:        l.Timestamp.UnixMicro(),
				Namespace: l.Namespace,
				Pod:       l.Pod,
				PodUID:    l.PodUID,
				Container: l.Container,
				Node:      l.NodeName,
				Stream:    l.Stream,
				TraceID:   traceID,
				IsJSON:    isJSON,
				Raw:       l.Content,
			}
			select {
			case out <- entry:
			case <-stop:
				return nil
			}
		}
	}
}
