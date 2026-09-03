package ui

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/iresharma/tracer/internal/collector/store"
)

// palette is a fixed, deterministic set of accessible colors used to badge
// services in the trace view. Assignment is a hash of "namespace/container"
// mod len(palette), so the same service always gets the same color within a
// single render (and typically across renders, since the palette is fixed).
var palette = []string{
	"#5b8def", "#e2794b", "#3fb68b", "#c364d8",
	"#e0b93e", "#4bb3e0", "#e0567c", "#8bc34a",
	"#d88e3f", "#6c7ae0", "#3ecfc0", "#e05b5b",
}

// durationField is the JSON key an application log may carry to indicate
// how long an operation took; if present, the trace view shows it as a
// small chip. Best-effort only — tracer has no real span start/end data.
const durationField = "duration_ms"

// levelFields are the JSON keys checked, in order, for a severity level.
var levelFields = []string{"level", "severity", "log.level", "loglevel"}

// LogRowView adds display-ready fields to a stored log row.
type LogRowView struct {
	store.LogRow
	TimeShort     string
	TimeFull      string
	ServiceLabel  string
	ServiceColor  string
	PrettyRaw     string
	DurationLabel string
	Level         string // "error" | "warn" | "info" | "debug" | "" (unknown)
}

// levelClass maps a level to the CSS class carrying its accent color.
// Colors are applied via static CSS class rules (app.css), not inline
// style values computed from data — html/template's CSS sanitizer rejects
// var(...) (and most other non-literal tokens) inside style attributes,
// silently replacing them with a "ZgotmplZ" sentinel, so severity colors
// must travel as a class name instead.
func levelClass(level string) string {
	switch level {
	case "error", "warn", "info", "debug":
		return "lvl-" + level
	default:
		return ""
	}
}

// LevelClass returns the CSS class carrying this row's severity accent
// color (empty for an unknown level, which renders with a neutral border).
func (v LogRowView) LevelClass() string { return levelClass(v.Level) }

// LevelClass returns the CSS class carrying this marker's severity accent
// color.
func (m TraceMarker) LevelClass() string { return levelClass(m.Level) }

func extractLevel(raw string, isJSON bool) string {
	if !isJSON {
		return ""
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ""
	}
	for _, key := range levelFields {
		raw, ok := v[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		s = strings.ToLower(s)
		switch {
		case strings.Contains(s, "err") || strings.Contains(s, "fatal") || strings.Contains(s, "panic"):
			return "error"
		case strings.Contains(s, "warn"):
			return "warn"
		case strings.Contains(s, "debug") || strings.Contains(s, "trace"):
			return "debug"
		case strings.Contains(s, "info"):
			return "info"
		default:
			return ""
		}
	}
	return ""
}

func serviceLabel(namespace, container string) string {
	return namespace + "/" + container
}

func serviceColor(namespace, container string) string {
	h := fnv.New32a()
	h.Write([]byte(serviceLabel(namespace, container)))
	return palette[int(h.Sum32())%len(palette)]
}

func prettyRaw(raw string, isJSON bool) string {
	if !isJSON {
		return raw
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(b)
}

func extractDuration(raw string, isJSON bool) string {
	if !isJSON {
		return ""
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ""
	}
	dv, ok := v[durationField]
	if !ok {
		return ""
	}
	var f float64
	if err := json.Unmarshal(dv, &f); err != nil {
		s := strings.Trim(string(dv), `"`)
		if s == "" {
			return ""
		}
		return s + "ms"
	}
	return fmt.Sprintf("%gms", f)
}

func toView(r store.LogRow) LogRowView {
	t := time.UnixMicro(r.TS)
	return LogRowView{
		LogRow:        r,
		TimeShort:     t.Format("15:04:05.000"),
		TimeFull:      t.Format(time.RFC3339Nano),
		ServiceLabel:  serviceLabel(r.Namespace, r.Container),
		ServiceColor:  serviceColor(r.Namespace, r.Container),
		PrettyRaw:     prettyRaw(r.Raw, r.IsJSON),
		DurationLabel: extractDuration(r.Raw, r.IsJSON),
		Level:         extractLevel(r.Raw, r.IsJSON),
	}
}

func toViews(rows []store.LogRow) []LogRowView {
	out := make([]LogRowView, len(rows))
	for i, r := range rows {
		out[i] = toView(r)
	}
	return out
}

// TraceRowView is a trace log line plus its position along the shared
// timeline, so the detail row list can stay visually in sync with the
// waterfall markers above it (both index by RowIndex).
type TraceRowView struct {
	LogRowView
	RowIndex    int
	PositionPct float64
}

// TraceMarker is one point on a lane's timeline track.
type TraceMarker struct {
	RowIndex    int
	PositionPct float64
	Level       string
}

// TraceLane groups a trace's log lines by service (namespace/container) for
// the waterfall view — the closest lightweight approximation to a real span
// timeline available from discrete log lines rather than span start/end
// data. Lane order follows first appearance, which for a causally-ordered
// trace usually matches call order (caller's lane appears before callee's).
type TraceLane struct {
	Service string
	Color   string
	Count   int
	Markers []TraceMarker
}

// TraceTick is one gridline/label on the waterfall's shared time axis,
// expressed as an offset from the trace's first log line rather than an
// absolute timestamp — "how far into the trace" is what matters when
// scanning a waterfall, not wall-clock time.
type TraceTick struct {
	PositionPct float64
	Label       string
}

const tickCount = 5

// buildTicks lays out evenly-spaced ticks across a span of microseconds.
func buildTicks(spanMicros int64) []TraceTick {
	ticks := make([]TraceTick, tickCount)
	for i := 0; i < tickCount; i++ {
		frac := float64(i) / float64(tickCount-1)
		ticks[i] = TraceTick{
			PositionPct: frac * 100,
			Label:       formatOffset(int64(float64(spanMicros) * frac)),
		}
	}
	return ticks
}

// formatOffset renders a microsecond offset at whichever unit keeps it
// readable — µs for sub-millisecond gaps, ms up to a second, then seconds.
func formatOffset(micros int64) string {
	switch {
	case micros == 0:
		return "0"
	case micros < 1000:
		return fmt.Sprintf("+%dµs", micros)
	case micros < 1_000_000:
		return fmt.Sprintf("+%dms", micros/1000)
	default:
		return fmt.Sprintf("+%.2fs", float64(micros)/1_000_000)
	}
}

// buildTraceView positions each row along a 0-100% timeline spanning the
// trace's earliest to latest timestamp, groups rows into per-service lanes
// for the waterfall visualization, and lays out the shared time axis.
func buildTraceView(views []LogRowView) (lanes []TraceLane, rows []TraceRowView, ticks []TraceTick, spanLabel string) {
	rows = make([]TraceRowView, len(views))
	if len(views) == 0 {
		return nil, rows, nil, ""
	}

	minTS, maxTS := views[0].TS, views[0].TS
	for _, v := range views {
		if v.TS < minTS {
			minTS = v.TS
		}
		if v.TS > maxTS {
			maxTS = v.TS
		}
	}
	span := maxTS - minTS

	laneIndex := map[string]int{}

	for i, v := range views {
		pct := 0.0
		if span > 0 {
			pct = float64(v.TS-minTS) / float64(span) * 100
		}
		rows[i] = TraceRowView{LogRowView: v, RowIndex: i, PositionPct: pct}

		idx, ok := laneIndex[v.ServiceLabel]
		if !ok {
			idx = len(lanes)
			laneIndex[v.ServiceLabel] = idx
			lanes = append(lanes, TraceLane{Service: v.ServiceLabel, Color: v.ServiceColor})
		}
		lanes[idx].Count++
		lanes[idx].Markers = append(lanes[idx].Markers, TraceMarker{RowIndex: i, PositionPct: pct, Level: v.Level})
	}

	return lanes, rows, buildTicks(span), strings.TrimPrefix(formatOffset(span), "+")
}
