package podwatcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSplitTimestampedLine(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantContent string
		wantZeroTS  bool
	}{
		{"well formed", "2026-09-03T00:17:09.669794202Z hello world\n", "hello world", false},
		{"no trailing newline", "2026-09-03T00:17:09.669794202Z hello world", "hello world", false},
		{"content with spaces", "2026-09-03T00:17:09.669794202Z {\"msg\":\"a b c\"}\n", `{"msg":"a b c"}`, false},
		{"no timestamp at all", "just a line with no ts prefix\n", "just a line with no ts prefix", true},
		{"malformed timestamp", "not-a-timestamp rest of line\n", "not-a-timestamp rest of line", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, content := splitTimestampedLine(tc.raw)
			if content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}
			isRecent := time.Since(ts) < time.Minute
			if tc.wantZeroTS && !isRecent {
				t.Errorf("expected fallback to ~now, got %v", ts)
			}
			if !tc.wantZeroTS && ts.Year() != 2026 {
				t.Errorf("expected parsed 2026 timestamp, got %v", ts)
			}
		})
	}
}

func TestStreamContainerLogsReadsLinesUntilEOF(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "2026-09-03T00:17:09.000000000Z line one\n")
		fmt.Fprint(w, "2026-09-03T00:17:09.100000000Z line two\n")
	}))
	defer srv.Close()

	cfg := &InClusterConfig{BaseURL: srv.URL}
	out := make(chan Line, 10)

	err := streamContainerLogs(context.Background(), srv.Client(), cfg, "demo", "app-1", "uid-1", "node-1", "app", out)
	if err != nil {
		t.Fatalf("streamContainerLogs: %v", err)
	}
	close(out)

	var got []Line
	for l := range out {
		got = append(got, l)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d: %+v", len(got), got)
	}
	if got[0].Content != "line one" || got[1].Content != "line two" {
		t.Errorf("unexpected content: %+v", got)
	}
	for _, l := range got {
		if l.Namespace != "demo" || l.Pod != "app-1" || l.PodUID != "uid-1" || l.NodeName != "node-1" || l.Container != "app" || l.Stream != "stdout" {
			t.Errorf("unexpected metadata on line: %+v", l)
		}
	}
}

func TestStreamContainerLogsRespectsContextCancellation(t *testing.T) {
	withFakeServiceAccount(t, "fake-token", "", "demo")

	blockUntilClosed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "2026-09-03T00:17:09.000000000Z first line\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-blockUntilClosed // simulate a long-lived follow=true connection
	}))
	defer srv.Close()
	defer close(blockUntilClosed)

	cfg := &InClusterConfig{BaseURL: srv.URL}
	out := make(chan Line, 10)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- streamContainerLogs(ctx, srv.Client(), cfg, "demo", "app-1", "uid-1", "node-1", "app", out)
	}()

	// Let the first line arrive, then cancel — the goroutine should exit
	// promptly rather than blocking forever on the still-open connection.
	<-out
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a context-cancellation error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamContainerLogs did not return promptly after context cancellation")
	}
}
