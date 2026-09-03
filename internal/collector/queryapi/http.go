// Package queryapi implements the collector's read-side JSON API
// (trace lookup, log browsing, and filter facets). These functions are
// also called directly by the HTML/HTMX UI handlers, so query logic lives
// in exactly one place (internal/collector/store).
package queryapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/iresharma/tracer/internal/collector/store"
)

type API struct {
	Store           *store.Store
	TraceWindowDays int
}

func New(s *store.Store, traceWindowDays int) *API {
	if traceWindowDays <= 0 {
		traceWindowDays = 2
	}
	return &API{Store: s, TraceWindowDays: traceWindowDays}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// TraceHandler serves GET /api/v1/trace/{id}?days=N
func (a *API) TraceHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing trace id", http.StatusBadRequest)
		return
	}
	days := a.TraceWindowDays
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	rows, err := a.Store.TraceByID(id, days)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// LogsHandler serves GET /api/v1/logs?namespace=&pod=&container=&day=YYYY-MM-DD&q=&limit=&cursor=
func (a *API) LogsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.BrowseFilter{
		Namespace: q.Get("namespace"),
		Pod:       q.Get("pod"),
		Container: q.Get("container"),
		Day:       q.Get("day"),
		Query:     q.Get("q"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("cursor"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.Cursor = n
		}
	}
	rows, err := a.Store.Browse(f)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// FacetsHandler serves GET /api/v1/facets
func (a *API) FacetsHandler(w http.ResponseWriter, r *http.Request) {
	f, err := a.Store.GetFacets()
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// HealthzHandler serves GET /healthz — liveness: process is up.
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// ReadyzHandler serves GET /readyz — readiness: db is reachable.
func ReadyzHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB().Ping(); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}
