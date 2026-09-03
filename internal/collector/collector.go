// Package collector wires together the storage, ingest, query, and UI
// layers into a single HTTP server.
package collector

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/iresharma/tracer/internal/collector/config"
	"github.com/iresharma/tracer/internal/collector/ingest"
	"github.com/iresharma/tracer/internal/collector/metrics"
	"github.com/iresharma/tracer/internal/collector/queryapi"
	"github.com/iresharma/tracer/internal/collector/store"
	"github.com/iresharma/tracer/internal/collector/ui"
)

type Server struct {
	cfg    config.Config
	store  *store.Store
	writer *store.Writer
	http   *http.Server
}

func New(cfg config.Config) (*Server, error) {
	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	s, err := store.Open(cfg.DBPath, cfg.SQLiteCacheKB)
	if err != nil {
		return nil, err
	}

	w := store.NewWriter(s, cfg.IngestChanCap, time.Duration(cfg.WriterFlushEvery)*time.Millisecond, cfg.WriterFlushAtSize)

	ingestHandler := ingest.NewHandler(w)
	api := queryapi.New(s, cfg.TraceWindowDays)
	uiHandlers := ui.New(s, cfg.TraceWindowDays)

	mux := http.NewServeMux()
	route(mux, "POST /api/v1/logs", ingestHandler)
	route(mux, "GET /api/v1/trace/{id}", http.HandlerFunc(api.TraceHandler))
	route(mux, "GET /api/v1/logs", http.HandlerFunc(api.LogsHandler))
	route(mux, "GET /api/v1/facets", http.HandlerFunc(api.FacetsHandler))
	mux.HandleFunc("GET /healthz", queryapi.HealthzHandler)
	mux.HandleFunc("GET /readyz", queryapi.ReadyzHandler(s))
	mux.Handle("GET /metrics", promhttp.Handler())

	route(mux, "GET /{$}", http.HandlerFunc(uiHandlers.Index))
	route(mux, "GET /logs", http.HandlerFunc(uiHandlers.LogsPage))
	route(mux, "GET /logs/results", http.HandlerFunc(uiHandlers.LogsResults))
	route(mux, "GET /trace", http.HandlerFunc(uiHandlers.TraceRedirect))
	route(mux, "GET /trace/{id}", http.HandlerFunc(uiHandlers.TraceView))
	route(mux, "GET /query", http.HandlerFunc(uiHandlers.QueryPage))
	route(mux, "POST /query/run", http.HandlerFunc(uiHandlers.QueryRun))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(ui.StaticFS())))

	return &Server{
		cfg:    cfg,
		store:  s,
		writer: w,
		http:   &http.Server{Addr: cfg.HTTPAddr, Handler: mux},
	}, nil
}

// Run starts the writer goroutine, the retention loop, and blocks serving
// HTTP until ctx is cancelled, then shuts everything down gracefully.
func (srv *Server) Run(ctx context.Context) error {
	go srv.writer.Run()
	go srv.retentionLoop(ctx)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("collector: listening on %s", srv.cfg.HTTPAddr)
		if err := srv.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Printf("collector: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.http.Shutdown(shutdownCtx); err != nil {
		log.Printf("collector: http shutdown error: %v", err)
	}
	srv.writer.Stop()
	return srv.store.Close()
}

func (srv *Server) retentionLoop(ctx context.Context) {
	// Run once at startup, then hourly.
	srv.prune()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			srv.prune()
		}
	}
}

func (srv *Server) prune() {
	timer := prometheus.NewTimer(metrics.RetentionPruneDuration)
	dropped, err := srv.store.PruneOlderThan(srv.cfg.RetentionDays)
	timer.ObserveDuration()
	if err != nil {
		log.Printf("collector: retention prune failed: %v", err)
		return
	}
	if len(dropped) > 0 {
		metrics.RetentionPartitionsDroppedTotal.Add(float64(len(dropped)))
		log.Printf("collector: pruned %d partition(s) older than %d days: %v", len(dropped), srv.cfg.RetentionDays, dropped)
	}
}

// route wraps h with request-count and duration instrumentation labeled by
// the exact pattern string it was registered under (not extracted from the
// request at runtime, since net/http doesn't expose the matched pattern on
// *http.Request) — cheap and avoids unbounded label cardinality from raw
// URL paths. Health/readiness/metrics/static routes are deliberately left
// uninstrumented: high-volume, low-value noise (scrapers polling
// themselves, asset requests).
func route(mux *http.ServeMux, pattern string, h http.Handler) {
	mux.Handle(pattern, instrument(pattern, h))
}

func instrument(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		timer := prometheus.NewTimer(metrics.HTTPRequestDuration.WithLabelValues(route))
		next.ServeHTTP(rec, r)
		timer.ObserveDuration()
		metrics.HTTPRequestsTotal.WithLabelValues(route, strconv.Itoa(rec.status)).Inc()
	})
}

// statusRecorder captures the status code written by the wrapped handler —
// http.ResponseWriter doesn't expose it otherwise.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
