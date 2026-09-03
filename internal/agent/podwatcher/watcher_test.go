package podwatcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWatcherRunEndToEnd exercises the full list -> watch -> per-container
// stream pipeline against a fake API server, verifying lines make it to
// Out with the right metadata and that Run stops promptly on cancellation.
func TestWatcherRunEndToEnd(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	blockUntilDone := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/namespaces/demo/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "true" {
			// Long-lived watch connection: block until the test ends
			// (mirrors a real watch that only closes on timeout/cancel).
			<-blockUntilDone
			return
		}
		fmt.Fprint(w, `{
			"metadata": {"resourceVersion": "1000"},
			"items": [
				{"metadata": {"name": "app-1", "uid": "uid-1", "namespace": "demo"},
				 "status": {"phase": "Running"},
				 "spec": {"nodeName": "node-a", "containers": [{"name": "app"}]}}
			]
		}`)
	})
	mux.HandleFunc("GET /api/v1/namespaces/demo/pods/app-1/log", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("container"); got != "app" {
			t.Errorf("log request missing container=app query param, got %q", got)
		}
		fmt.Fprint(w, "2026-09-03T00:17:09.000000000Z hello from app-1\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-blockUntilDone // simulate follow=true staying open
	})

	srv := httptest.NewServer(mux)
	// Registered in this order so, on LIFO teardown, blockUntilDone is
	// closed (unblocking the handlers above) *before* srv.Close() — which
	// otherwise blocks until all outstanding requests finish, deadlocking
	// against handlers that are waiting on this same channel.
	defer srv.Close()
	defer close(blockUntilDone)

	cfg := &InClusterConfig{BaseURL: srv.URL}
	out := make(chan Line, 10)
	watcher := New(cfg, srv.Client(), "demo", out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()

	select {
	case l := <-out:
		if l.Namespace != "demo" || l.Pod != "app-1" || l.PodUID != "uid-1" || l.Container != "app" || l.NodeName != "node-a" {
			t.Errorf("unexpected line metadata: %+v", l)
		}
		if l.Content != "hello from app-1" {
			t.Errorf("content = %q", l.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive a log line within 3s")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned an error after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return promptly after context cancellation")
	}
}

// TestWatcherStopsStreamOnPodDeleted verifies a DELETED watch event tears
// down that pod's log-stream goroutine (observed via the exported
// streams-active count going back to 0) rather than leaking it.
func TestWatcherStopsStreamOnPodDeleted(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	blockWatch := make(chan struct{})
	watchEvents := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/namespaces/demo/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "true" {
			// Wait for the test to signal "stream is confirmed up", then
			// send a DELETED event, then hang until the test tears down.
			<-watchEvents
			fmt.Fprintln(w, `{"type":"DELETED","object":{"metadata":{"name":"app-1","uid":"uid-1","namespace":"demo"},"status":{"phase":"Running"},"spec":{"containers":[{"name":"app"}]}}}`)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-blockWatch
			return
		}
		fmt.Fprint(w, `{"metadata":{"resourceVersion":"1000"},"items":[
			{"metadata":{"name":"app-1","uid":"uid-1","namespace":"demo"},
			 "status":{"phase":"Running"},
			 "spec":{"nodeName":"node-a","containers":[{"name":"app"}]}}
		]}`)
	})
	mux.HandleFunc("GET /api/v1/namespaces/demo/pods/app-1/log", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "2026-09-03T00:17:09.000000000Z first\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // stream ends when the watcher cancels it on DELETED
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(blockWatch)

	cfg := &InClusterConfig{BaseURL: srv.URL}
	out := make(chan Line, 10)
	watcher := New(cfg, srv.Client(), "demo", out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watcher.Run(ctx)

	<-out // confirm the stream actually started before we delete the pod
	close(watchEvents)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		watcher.mu.Lock()
		n := len(watcher.cancels)
		watcher.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("stream was not stopped after the pod's DELETED event")
}
