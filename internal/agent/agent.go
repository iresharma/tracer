// Package agent wires discovery, tailing, parsing, batching, and
// forwarding into the DaemonSet agent's log collection pipeline.
package agent

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/iresharma/tracer/internal/agent/batcher"
	"github.com/iresharma/tracer/internal/agent/checkpoint"
	"github.com/iresharma/tracer/internal/agent/config"
	"github.com/iresharma/tracer/internal/agent/discovery"
	"github.com/iresharma/tracer/internal/agent/forwarder"
	"github.com/iresharma/tracer/internal/agent/parser"
	"github.com/iresharma/tracer/internal/agent/tailer"
	"github.com/iresharma/tracer/internal/model"
)

type Agent struct {
	cfg config.Config
	cp  *checkpoint.Store
}

func New(cfg config.Config) (*Agent, error) {
	cp, err := checkpoint.Load(cfg.CheckpointPath)
	if err != nil {
		return nil, err
	}
	return &Agent{cfg: cfg, cp: cp}, nil
}

// Run blocks the whole collection pipeline until ctx is cancelled, then
// drains outstanding work (final batch flush, final checkpoint write)
// before returning. The actual log source — hostPath tailing or the
// Kubernetes API — is picked by IngestionMode; everything downstream of
// "a complete log line with metadata" (batching, forwarding, retry) is
// shared between both modes.
func (a *Agent) Run(ctx context.Context) error {
	stop := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stop)
	}()

	entries := make(chan model.LogEntry, 1000)

	fwd := forwarder.New(forwarder.Options{
		CollectorURL: a.cfg.CollectorURL,
		AgentID:      a.cfg.NodeName,
		RingCapacity: a.cfg.RingCapacity,
	})
	b := batcher.New(entries, batcher.Options{
		MaxBatchSize:  a.cfg.MaxBatchSize,
		MaxBatchBytes: a.cfg.MaxBatchBytes,
		FlushInterval: a.cfg.FlushInterval,
	}, fwd.Submit)

	metricsSrv := a.startMetricsServer()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() { defer wg.Done(); fwd.Run(stop) }()

	wg.Add(1)
	go func() { defer wg.Done(); b.Run(stop) }()

	if a.cfg.IngestionMode == "api" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runAPIMode(ctx, entries, stop); err != nil {
				log.Printf("agent: api mode exited: %v", err)
			}
		}()
	} else {
		found := make(chan string, 1000)
		lines := make(chan tailer.Line, 1000)
		scanner := discovery.New(a.cfg.LogRoot, a.cfg.RescanInterval, found)

		wg.Add(1)
		go func() { defer wg.Done(); scanner.Run(stop) }()

		wg.Add(1)
		go func() { defer wg.Done(); a.cp.RunPeriodicFlush(a.cfg.CheckpointFlush, stop) }()

		wg.Add(1)
		go func() { defer wg.Done(); a.enrich(lines, entries, stop) }()

		wg.Add(1)
		go func() { defer wg.Done(); a.spawnTailers(ctx, found, lines) }()
	}

	<-ctx.Done()
	wg.Wait()
	if metricsSrv != nil {
		_ = metricsSrv.Close()
	}
	return nil
}

// startMetricsServer starts a minimal HTTP server serving /metrics
// (Prometheus) and /healthz on its own port, separate from the collector
// forwarding path — the agent otherwise has no HTTP listener at all.
// Errors starting it are logged, not fatal: metrics are diagnostic, losing
// them shouldn't take down log collection itself.
func (a *Agent) startMetricsServer() *http.Server {
	srv := &http.Server{Addr: a.cfg.MetricsAddr, Handler: metricsMux()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("agent: metrics server error: %v", err)
		}
	}()
	log.Printf("agent: metrics listening on %s", a.cfg.MetricsAddr)
	return srv
}

// metricsMux is factored out so it can be exercised directly in tests
// without binding a real port.
func metricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return mux
}

func (a *Agent) spawnTailers(ctx context.Context, found <-chan string, lines chan<- tailer.Line) {
	opts := tailer.Options{
		PollInterval: a.cfg.PollInterval,
		MaxLineBytes: a.cfg.MaxLineBytes,
		StartAtEnd:   a.cfg.StartAtEnd,
	}
	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case path, ok := <-found:
			if !ok {
				wg.Wait()
				return
			}
			tl := tailer.New(path, opts, a.cp, lines)
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				if err := tl.Run(ctx); err != nil {
					log.Printf("agent: tailer for %s exited: %v", path, err)
				}
			}(path)
		}
	}
}

func (a *Agent) enrich(lines <-chan tailer.Line, out chan<- model.LogEntry, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case l, ok := <-lines:
			if !ok {
				return
			}
			meta, err := parser.ParsePodLogPath(l.Path)
			if err != nil {
				log.Printf("agent: could not derive metadata from %s: %v (dropping line)", l.Path, err)
				continue
			}
			isJSON, traceID := parser.ExtractTraceID(l.Content, a.cfg.TraceIDField)
			entry := model.LogEntry{
				TS:        l.Timestamp.UnixMicro(),
				Namespace: meta.Namespace,
				Pod:       meta.Pod,
				PodUID:    meta.PodUID,
				Container: meta.Container,
				Node:      a.cfg.NodeName,
				Stream:    l.Stream,
				TraceID:   traceID,
				IsJSON:    isJSON,
				Raw:       l.Content,
			}
			select {
			case out <- entry:
			case <-stop:
				return
			}
		}
	}
}
