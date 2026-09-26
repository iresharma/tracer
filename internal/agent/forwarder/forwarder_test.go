package forwarder

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iresharma/tracer/internal/model"
)

func TestForwarderSendsSuccessfully(t *testing.T) {
	var received int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	f := New(Options{CollectorURL: srv.URL, AgentID: "node-1", MinBackoff: 10 * time.Millisecond})
	stop := make(chan struct{})
	go f.Run(stop)
	defer close(stop)

	f.Submit([]model.LogEntry{{Raw: "hello"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&received) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected collector to receive at least one batch")
}

func TestForwarderRetriesOnCollectorDown(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var received int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	f := New(Options{CollectorURL: srv.URL, AgentID: "node-1", MinBackoff: 20 * time.Millisecond, MaxBackoff: 50 * time.Millisecond})
	stop := make(chan struct{})
	go f.Run(stop)
	defer close(stop)

	f.Submit([]model.LogEntry{{Raw: "hello"}})
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&received) != 0 {
		t.Fatalf("expected no successful delivery yet while collector is down")
	}

	fail.Store(false)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&received) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected batch to eventually be delivered once collector recovers")
}

func TestForwarderSendsIngestAuthHeader(t *testing.T) {
	tests := []struct {
		name          string
		token         string // Options.IngestAuthToken
		wantHeader    string // expected Authorization header value; empty means no header
		wantHeaderSet bool
	}{
		{
			name:          "no token configured sends no Authorization header",
			token:         "",
			wantHeaderSet: false,
		},
		{
			name:          "token configured sends Bearer header",
			token:         "shared-secret",
			wantHeader:    "Bearer shared-secret",
			wantHeaderSet: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotHeader string
			var gotSet bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get("Authorization")
				_, gotSet = r.Header["Authorization"]
				w.WriteHeader(http.StatusAccepted)
			}))
			defer srv.Close()

			f := New(Options{
				CollectorURL:    srv.URL,
				AgentID:         "node-1",
				MinBackoff:      10 * time.Millisecond,
				IngestAuthToken: tc.token,
			})
			stop := make(chan struct{})
			go f.Run(stop)
			defer close(stop)

			f.Submit([]model.LogEntry{{Raw: "hello"}})

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && !gotSet && tc.wantHeaderSet {
				time.Sleep(10 * time.Millisecond)
			}
			// Give the non-auth case a moment too, since the assertion
			// below needs at least one request to have landed.
			if !gotSet && !tc.wantHeaderSet {
				time.Sleep(100 * time.Millisecond)
			}

			if gotSet != tc.wantHeaderSet {
				t.Fatalf("Authorization header present = %v, want %v", gotSet, tc.wantHeaderSet)
			}
			if tc.wantHeaderSet && gotHeader != tc.wantHeader {
				t.Fatalf("Authorization header = %q, want %q", gotHeader, tc.wantHeader)
			}
		})
	}
}

func TestRingDropsOldestWhenFull(t *testing.T) {
	r := newRing(2)
	r.Push([]model.LogEntry{{Raw: "1"}})
	r.Push([]model.LogEntry{{Raw: "2"}})
	r.Push([]model.LogEntry{{Raw: "3"}}) // should drop "1"

	if r.Len() != 2 {
		t.Fatalf("expected ring length 2, got %d", r.Len())
	}
	if r.Dropped() != 1 {
		t.Fatalf("expected 1 dropped batch, got %d", r.Dropped())
	}
	batch, ok := r.Peek()
	if !ok || batch[0].Raw != "2" {
		t.Fatalf("expected oldest surviving batch to be %q, got %+v", "2", batch)
	}
}
