package podwatcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// listPods does a plain (non-watch) LIST, used both for the initial sync
// (so already-running pods aren't missed — a bare WATCH only delivers
// *changes* from the moment it opens) and to re-sync after a watch
// connection drops.
func listPods(ctx context.Context, client *http.Client, cfg *InClusterConfig, ns string) (*podList, error) {
	req, err := cfg.NewRequest("/api/v1/namespaces/" + ns + "/pods")
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d listing pods in %s", resp.StatusCode, ns)
	}

	var list podList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode pod list: %w", err)
	}
	return &list, nil
}

// watchPods opens a watch=true connection starting from resourceVersion
// and calls handle for every ADDED/MODIFIED/DELETED event until ctx is
// cancelled or the connection ends (e.g. the standard ~30m watch timeout
// the API server itself enforces) — the caller is expected to re-LIST and
// re-WATCH when this returns nil.
func watchPods(ctx context.Context, client *http.Client, cfg *InClusterConfig, ns, resourceVersion string, handle func(eventType string, pod podMeta)) error {
	path := "/api/v1/namespaces/" + ns + "/pods?" + url.Values{
		"watch":           {"true"},
		"resourceVersion": {resourceVersion},
	}.Encode()

	req, err := cfg.NewRequest(path)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d watching pods in %s", resp.StatusCode, ns)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Pod objects can be large (many env vars, long init container specs,
	// etc); the default 64KB scanner buffer is tight for that. 1MB per
	// line is generous headroom without being unbounded.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var ev watchEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue // malformed event; skip rather than abort the whole watch
		}
		if ev.Type == "ERROR" || ev.Type == "BOOKMARK" {
			continue
		}
		var pod podMeta
		if err := json.Unmarshal(ev.Object, &pod); err != nil {
			continue
		}
		handle(ev.Type, pod)
	}
	return scanner.Err()
}
