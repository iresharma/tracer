// Package model defines the wire contract shared by the agent and the collector.
package model

// LogEntry is a single log line enriched with metadata derived from the
// container log file path (namespace/pod/container/node), plus whatever the
// agent could extract from the line itself.
type LogEntry struct {
	// TS is unix micros. Microsecond resolution (not millis) keeps
	// causally-ordered, same-millisecond log lines — e.g. fast local
	// service-to-service calls — from tying and sorting arbitrarily in
	// the trace view.
	TS        int64  `json:"ts"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	PodUID    string `json:"pod_uid"`
	Container string `json:"container"`
	Node      string `json:"node"`
	Stream    string `json:"stream"` // "stdout" | "stderr"
	TraceID   string `json:"trace_id,omitempty"`
	IsJSON    bool   `json:"is_json"`
	Raw       string `json:"raw"`
}

// Batch is the request body posted by an agent to the collector's ingest API.
type Batch struct {
	AgentID string     `json:"agent_id"`
	Entries []LogEntry `json:"entries"`
}
