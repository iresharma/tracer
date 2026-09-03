// Package collector wires together the storage, ingest, query, and UI
// layers into a single HTTP server.
package collector

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/iresharma/tracer/internal/collector/config"
	"github.com/iresharma/tracer/internal/collector/ingest"
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
	mux.Handle("POST /api/v1/logs", ingestHandler)
	mux.HandleFunc("GET /api/v1/trace/{id}", api.TraceHandler)
	mux.HandleFunc("GET /api/v1/logs", api.LogsHandler)
	mux.HandleFunc("GET /api/v1/facets", api.FacetsHandler)
	mux.HandleFunc("GET /healthz", queryapi.HealthzHandler)
	mux.HandleFunc("GET /readyz", queryapi.ReadyzHandler(s))

	mux.HandleFunc("GET /{$}", uiHandlers.Index)
	mux.HandleFunc("GET /logs", uiHandlers.LogsPage)
	mux.HandleFunc("GET /logs/results", uiHandlers.LogsResults)
	mux.HandleFunc("GET /trace", uiHandlers.TraceRedirect)
	mux.HandleFunc("GET /trace/{id}", uiHandlers.TraceView)
	mux.HandleFunc("GET /query", uiHandlers.QueryPage)
	mux.HandleFunc("POST /query/run", uiHandlers.QueryRun)
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
	dropped, err := srv.store.PruneOlderThan(srv.cfg.RetentionDays)
	if err != nil {
		log.Printf("collector: retention prune failed: %v", err)
		return
	}
	if len(dropped) > 0 {
		log.Printf("collector: pruned %d partition(s) older than %d days: %v", len(dropped), srv.cfg.RetentionDays, dropped)
	}
}
