// Package ingest implements the collector's log-batch ingestion endpoint.
package ingest

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/iresharma/tracer/internal/collector/metrics"
	"github.com/iresharma/tracer/internal/collector/store"
	"github.com/iresharma/tracer/internal/model"
)

const (
	maxBodyBytes  = 4 << 20 // 4MB decompressed
	maxBatchLines = 2000    // defense in depth; agent's own batcher caps at 500
)

// Handler serves POST /api/v1/logs.
type Handler struct {
	writer *store.Writer
}

func NewHandler(w *store.Writer) *Handler {
	return &Handler{writer: w}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "invalid gzip body", http.StatusBadRequest)
			return
		}
		defer gz.Close()
		body = gz
	}
	body = io.LimitReader(body, maxBodyBytes+1)

	var batch model.Batch
	dec := json.NewDecoder(body)
	if err := dec.Decode(&batch); err != nil {
		http.Error(w, "invalid batch json", http.StatusBadRequest)
		return
	}
	if len(batch.Entries) > maxBatchLines {
		http.Error(w, "batch too large", http.StatusBadRequest)
		return
	}

	metrics.IngestBatchesTotal.Inc()

	accepted, rejected := 0, 0
	full := false
	for _, e := range batch.Entries {
		if e.Raw == "" {
			rejected++
			metrics.IngestEntriesTotal.WithLabelValues("rejected").Inc()
			continue
		}
		if h.writer.Enqueue(e) {
			accepted++
			metrics.IngestEntriesTotal.WithLabelValues("accepted").Inc()
		} else {
			full = true
			metrics.IngestEntriesTotal.WithLabelValues("queue_full").Inc()
			break
		}
	}

	resp := map[string]int{"accepted": accepted, "rejected": rejected}
	if full {
		// Collector is falling behind; tell the agent to back off. This
		// couples agent-side backpressure to actual collector health.
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("ingest: encode response: %v", err)
		}
		return
	}

	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ingest: encode response: %v", err)
	}
}
