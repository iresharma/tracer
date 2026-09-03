// Package ui implements the collector's server-rendered pages (HTMX +
// Alpine.js, no SPA build step). Handlers call the same store query
// functions used by the JSON API in internal/collector/queryapi, so query
// logic lives in exactly one place.
package ui

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iresharma/tracer/internal/collector/store"
)

type Handlers struct {
	Store           *store.Store
	TraceWindowDays int
}

func New(s *store.Store, traceWindowDays int) *Handlers {
	if traceWindowDays <= 0 {
		traceWindowDays = 2
	}
	return &Handlers{Store: s, TraceWindowDays: traceWindowDays}
}

type indexData struct {
	Active       string
	TodayCount   int
	ServiceCount int
	OldestDay    string
	RecentRows   []LogRowView
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Store.Browse(store.BrowseFilter{Limit: 50})
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	facets, err := h.Store.GetFacets()
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	oldest := "-"
	if partitions, err := h.Store.ListPartitions(); err == nil && len(partitions) > 0 {
		oldest = partitions[0]
	}
	data := indexData{
		Active:       "home",
		TodayCount:   len(rows),
		ServiceCount: len(facets.Containers),
		OldestDay:    oldest,
		RecentRows:   toViews(rows),
	}
	if err := indexTmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type resultsView struct {
	Rows      []LogRowView
	Namespace string
	Pod       string
	Container string
	Day       string
	Query     string
	LastID    int64
	HasMore   bool
}

type logsData struct {
	Active    string
	Facets    *store.Facets
	Namespace string
	Pod       string
	Container string
	Day       string
	Query     string
	Results   resultsView
}

func filterFromRequest(r *http.Request) store.BrowseFilter {
	q := r.URL.Query()
	f := store.BrowseFilter{
		Namespace: q.Get("namespace"),
		Pod:       q.Get("pod"),
		Container: q.Get("container"),
		Day:       q.Get("day"),
		Query:     q.Get("q"),
		Limit:     100,
	}
	if v := q.Get("cursor"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.Cursor = n
		}
	}
	return f
}

func (h *Handlers) buildResultsView(f store.BrowseFilter) (resultsView, error) {
	rows, err := h.Store.Browse(f)
	if err != nil {
		return resultsView{}, err
	}
	views := toViews(rows)
	rv := resultsView{
		Rows:      views,
		Namespace: f.Namespace,
		Pod:       f.Pod,
		Container: f.Container,
		Day:       f.Day,
		Query:     f.Query,
		HasMore:   len(rows) == f.Limit && f.Limit > 0,
	}
	if len(rows) > 0 {
		rv.LastID = rows[len(rows)-1].ID
	}
	return rv, nil
}

func (h *Handlers) LogsPage(w http.ResponseWriter, r *http.Request) {
	f := filterFromRequest(r)
	if f.Day == "" {
		f.Day = time.Now().Format("2006-01-02")
	}
	results, err := h.buildResultsView(f)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	facets, err := h.Store.GetFacets()
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	data := logsData{
		Active:    "logs",
		Facets:    facets,
		Namespace: f.Namespace,
		Pod:       f.Pod,
		Container: f.Container,
		Day:       f.Day,
		Query:     f.Query,
		Results:   results,
	}
	if err := logsTmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handlers) LogsResults(w http.ResponseWriter, r *http.Request) {
	f := filterFromRequest(r)
	results, err := h.buildResultsView(f)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if err := resultsTmpl.ExecuteTemplate(w, "results", results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// TraceRedirect handles the lookup-box form submission (GET /trace?id=...)
// and redirects to the bookmarkable /trace/{id} URL.
func (h *Handlers) TraceRedirect(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/trace/"+id, http.StatusFound)
}

type traceData struct {
	Active       string
	TraceID      string
	Rows         []TraceRowView
	Lanes        []TraceLane
	Ticks        []TraceTick
	ServiceCount int
	Span         string
	WindowDays   int
}

func (h *Handlers) TraceView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	storeRows, err := h.Store.TraceByID(id, h.TraceWindowDays)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	views := toViews(storeRows)
	lanes, rows, ticks, span := buildTraceView(views)
	data := traceData{Active: "logs", TraceID: id, Rows: rows, Lanes: lanes, Ticks: ticks, Span: span, WindowDays: h.TraceWindowDays}
	if len(views) > 0 {
		data.ServiceCount = len(lanes)
	}
	if err := traceTmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type queryData struct {
	Active   string
	SQL      string
	Tables   []string
	Examples []queryExample
	Result   *queryResultData
}

type queryExample struct {
	Label string
	SQL   string
}

type queryResultData struct {
	Columns   []string
	Rows      [][]string
	Truncated bool
	ErrMsg    string
}

// defaultQuerySQL suggests a starting query against today's partition, so
// the page isn't a blank textbox on first visit.
func defaultQuerySQL() string {
	return "SELECT ts, namespace, pod, container, trace_id, raw\nFROM logs_" + time.Now().UTC().Format("20060102") + "\nORDER BY id DESC\nLIMIT 50"
}

// recentTableNames lists day-partition table names, most recent first, for
// the query page's schema hint — a user typing SQL needs to know what the
// tables are actually called.
func recentTableNames(s *store.Store) []string {
	partitions, err := s.ListPartitions()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(partitions)+1)
	names = append(names, "day_partitions")
	for i := len(partitions) - 1; i >= 0; i-- {
		names = append(names, "logs_"+partitions[i])
	}
	return names
}

// jsonQueryExamples suggests a few json_extract / ->> patterns against the
// given table, so SQLite's built-in JSON querying (no schema needed — it
// works directly against the raw TEXT column) is discoverable rather than
// something a user has to already know to type.
//
// Every example filters on is_json = 1 first: tracer deliberately stores
// non-JSON log lines too (anything the agent couldn't parse as JSON is
// still kept for plain browsing), and SQLite's JSON functions raise a hard
// "malformed JSON" error — not NULL — the moment they hit one of those
// rows, which aborts the whole query rather than just that row.
func jsonQueryExamples(table string) []queryExample {
	return []queryExample{
		{
			Label: "Pull fields out of raw JSON",
			SQL:   "SELECT ts, raw->>'service' AS service, raw->>'level' AS level, raw->>'msg' AS msg FROM " + table + " WHERE is_json = 1 ORDER BY id DESC LIMIT 50",
		},
		{
			Label: "Filter on a JSON field",
			SQL:   "SELECT * FROM " + table + " WHERE is_json = 1 AND raw->>'level' = 'error' ORDER BY id DESC LIMIT 50",
		},
		{
			Label: "Group by a JSON field",
			SQL:   "SELECT raw->>'service' AS service, COUNT(*) AS n FROM " + table + " WHERE is_json = 1 GROUP BY service ORDER BY n DESC",
		},
		{
			Label: "Nested path + numeric field",
			SQL:   "SELECT trace_id, CAST(raw->>'duration_ms' AS INTEGER) AS duration_ms FROM " + table + " WHERE is_json = 1 AND raw->>'duration_ms' IS NOT NULL ORDER BY duration_ms DESC LIMIT 20",
		},
	}
}

func (h *Handlers) QueryPage(w http.ResponseWriter, r *http.Request) {
	tables := recentTableNames(h.Store)
	exampleTable := "logs_" + time.Now().UTC().Format("20060102")
	for _, t := range tables {
		if strings.HasPrefix(t, "logs_") {
			exampleTable = t
			break
		}
	}
	data := queryData{
		Active:   "query",
		SQL:      defaultQuerySQL(),
		Tables:   tables,
		Examples: jsonQueryExamples(exampleTable),
	}
	if err := queryTmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handlers) QueryRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	sqlText := r.FormValue("sql")
	result := &queryResultData{}
	res, err := h.Store.RunReadOnlyQuery(r.Context(), sqlText)
	if err != nil {
		result.ErrMsg = err.Error()
	} else {
		result.Columns = res.Columns
		result.Rows = res.Rows
		result.Truncated = res.Truncated
	}
	if err := queryResultsTmpl.ExecuteTemplate(w, "query_results", result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
