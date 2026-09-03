package podwatcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPods(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/v1/namespaces/demo/pods" {
			t.Errorf("unexpected path: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-token" {
			t.Errorf("unexpected Authorization header: %s", got)
		}
		fmt.Fprint(w, `{
			"metadata": {"resourceVersion": "1000"},
			"items": [
				{"metadata": {"name": "app-1", "uid": "uid-1", "namespace": "demo"},
				 "status": {"phase": "Running"},
				 "spec": {"nodeName": "node-a", "containers": [{"name": "app"}]}}
			]
		}`)
	}))
	defer srv.Close()

	cfg := &InClusterConfig{BaseURL: srv.URL}
	list, err := listPods(context.Background(), srv.Client(), cfg, "demo")
	if err != nil {
		t.Fatalf("listPods: %v", err)
	}
	if list.Metadata.ResourceVersion != "1000" {
		t.Errorf("resourceVersion = %q", list.Metadata.ResourceVersion)
	}
	if len(list.Items) != 1 || list.Items[0].Metadata.Name != "app-1" {
		t.Fatalf("unexpected items: %+v", list.Items)
	}
	if !list.Items[0].running() {
		t.Error("expected pod to be considered running")
	}
}

func TestListPodsNonOKStatus(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := &InClusterConfig{BaseURL: srv.URL}
	if _, err := listPods(context.Background(), srv.Client(), cfg, "demo"); err == nil {
		t.Fatal("expected an error for a 403 response")
	}
}

func TestWatchPodsDispatchesEventsAndSkipsNoise(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("resourceVersion"); got != "1000" {
			t.Errorf("resourceVersion query param = %q", got)
		}
		lines := []string{
			`{"type":"BOOKMARK","object":{}}`,
			`{"type":"ERROR","object":{}}`,
			`{"type":"ADDED","object":{"metadata":{"name":"app-1","uid":"uid-1","namespace":"demo"},"status":{"phase":"Pending"},"spec":{"containers":[{"name":"app"}]}}}`,
			`{"type":"MODIFIED","object":{"metadata":{"name":"app-1","uid":"uid-1","namespace":"demo"},"status":{"phase":"Running"},"spec":{"containers":[{"name":"app"}]}}}`,
			`{"type":"DELETED","object":{"metadata":{"name":"app-1","uid":"uid-1","namespace":"demo"},"status":{"phase":"Running"},"spec":{"containers":[{"name":"app"}]}}}`,
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}))
	defer srv.Close()

	cfg := &InClusterConfig{BaseURL: srv.URL}
	var events []string
	err := watchPods(context.Background(), srv.Client(), cfg, "demo", "1000", func(eventType string, pod podMeta) {
		events = append(events, eventType+":"+pod.Status.Phase)
	})
	if err != nil {
		t.Fatalf("watchPods: %v", err)
	}
	want := []string{"ADDED:Pending", "MODIFIED:Running", "DELETED:Running"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event[%d] = %q, want %q", i, events[i], want[i])
		}
	}
}

func TestWatchPodsSkipsMalformedLines(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `not valid json at all`)
		fmt.Fprintln(w, `{"type":"ADDED","object":{"metadata":{"name":"ok","uid":"u","namespace":"demo"},"status":{"phase":"Running"},"spec":{"containers":[]}}}`)
	}))
	defer srv.Close()

	cfg := &InClusterConfig{BaseURL: srv.URL}
	var names []string
	err := watchPods(context.Background(), srv.Client(), cfg, "demo", "1000", func(eventType string, pod podMeta) {
		names = append(names, pod.Metadata.Name)
	})
	if err != nil {
		t.Fatalf("watchPods: %v", err)
	}
	if len(names) != 1 || names[0] != "ok" {
		t.Fatalf("expected the malformed line to be skipped, got names=%v", names)
	}
}
