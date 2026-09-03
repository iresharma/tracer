package podwatcher

import "encoding/json"

// podMeta is the subset of a Pod object this package actually needs —
// deliberately not the full corev1.Pod shape, since we're not using
// client-go's types either (see client.go's doc comment).
type podMeta struct {
	Metadata struct {
		Name      string `json:"name"`
		UID       string `json:"uid"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name string `json:"name"`
		} `json:"containers"`
	} `json:"spec"`
}

func (p podMeta) running() bool { return p.Status.Phase == "Running" }

// podList is the response body of a plain (non-watch) LIST call.
type podList struct {
	Metadata struct {
		ResourceVersion string `json:"resourceVersion"`
	} `json:"metadata"`
	Items []podMeta `json:"items"`
}

// watchEvent is one line of a `watch=true` response body — the body is a
// stream of these, newline-delimited, one JSON object per Kubernetes API
// change (not a JSON array).
type watchEvent struct {
	Type   string          `json:"type"` // ADDED | MODIFIED | DELETED | ERROR | BOOKMARK
	Object json.RawMessage `json:"object"`
}
