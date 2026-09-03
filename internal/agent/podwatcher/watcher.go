package podwatcher

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/iresharma/tracer/internal/agent/metrics"
)

const (
	streamMinBackoff = time.Second
	streamMaxBackoff = 30 * time.Second
	watchMinBackoff  = time.Second
	watchMaxBackoff  = 30 * time.Second
)

// Watcher watches Pods in one namespace and keeps a follow=true log stream
// open for every container of every Running pod, emitting complete lines
// on Out. It never needs hostPath or node access — only the Kubernetes API
// (list/watch pods, get pods/log), all scoped to Namespace.
type Watcher struct {
	Config    *InClusterConfig
	Client    *http.Client
	Namespace string
	Out       chan<- Line

	mu      sync.Mutex
	cancels map[string]context.CancelFunc // key: podUID + "/" + container
}

func New(cfg *InClusterConfig, client *http.Client, namespace string, out chan<- Line) *Watcher {
	return &Watcher{
		Config:    cfg,
		Client:    client,
		Namespace: namespace,
		Out:       out,
		cancels:   map[string]context.CancelFunc{},
	}
}

// Run blocks, re-establishing the LIST+WATCH connection with backoff
// whenever it drops, until ctx is cancelled. Stream goroutines it starts
// are children of ctx directly (not of any single watch cycle), so a
// watch-connection reconnect never interrupts already-open log streams.
func (w *Watcher) Run(ctx context.Context) error {
	backoff := watchMinBackoff
	for {
		err := w.runOnce(ctx)
		if ctx.Err() != nil {
			w.stopAll()
			return nil
		}
		if err != nil {
			log.Printf("podwatcher: watch connection ended: %v; reconnecting in %s", err, backoff)
		}
		metrics.PodwatcherWatchReconnectsTotal.Inc()
		select {
		case <-ctx.Done():
			w.stopAll()
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > watchMaxBackoff {
			backoff = watchMaxBackoff
		}
		if err == nil {
			backoff = watchMinBackoff // a clean end (e.g. server-side watch timeout) isn't a failure
		}
	}
}

func (w *Watcher) runOnce(ctx context.Context) error {
	list, err := listPods(ctx, w.Client, w.Config, w.Namespace)
	if err != nil {
		return err
	}
	for _, pod := range list.Items {
		w.reconcile(ctx, "ADDED", pod)
	}
	return watchPods(ctx, w.Client, w.Config, w.Namespace, list.Metadata.ResourceVersion, func(eventType string, pod podMeta) {
		w.reconcile(ctx, eventType, pod)
	})
}

// reconcile starts or stops per-container stream goroutines to match one
// pod's current state. Idempotent: MODIFIED events fire frequently (status
// updates, etc), so this only acts on transitions — already-streaming
// containers are left alone, not restarted.
func (w *Watcher) reconcile(ctx context.Context, eventType string, pod podMeta) {
	if eventType == "DELETED" {
		w.stopPod(pod.Metadata.UID)
		return
	}
	if !pod.running() {
		return
	}
	for _, c := range pod.Spec.Containers {
		w.ensureStream(ctx, pod, c.Name)
	}
}

func (w *Watcher) ensureStream(ctx context.Context, pod podMeta, container string) {
	key := pod.Metadata.UID + "/" + container

	w.mu.Lock()
	_, exists := w.cancels[key]
	if exists {
		w.mu.Unlock()
		return
	}
	streamCtx, cancel := context.WithCancel(ctx)
	w.cancels[key] = cancel
	w.mu.Unlock()

	metrics.PodwatcherStreamsActive.Inc()
	go w.runStream(streamCtx, pod.Metadata.Namespace, pod.Metadata.Name, pod.Metadata.UID, pod.Spec.NodeName, container, key)
}

// runStream keeps one container's log stream open, reconnecting with
// backoff if it ends while the pod is still known to be running (e.g. the
// container itself restarted) — it only truly stops when streamCtx is
// cancelled, which happens via stopPod on a DELETED event.
func (w *Watcher) runStream(streamCtx context.Context, ns, pod, podUID, nodeName, container, key string) {
	defer func() {
		w.mu.Lock()
		delete(w.cancels, key)
		w.mu.Unlock()
		metrics.PodwatcherStreamsActive.Dec()
	}()

	backoff := streamMinBackoff
	for {
		err := streamContainerLogs(streamCtx, w.Client, w.Config, ns, pod, podUID, nodeName, container, w.Out)
		if streamCtx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("podwatcher: stream %s/%s/%s ended: %v; retrying in %s", ns, pod, container, err, backoff)
		}
		metrics.PodwatcherStreamReconnectsTotal.Inc()
		select {
		case <-streamCtx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > streamMaxBackoff {
			backoff = streamMaxBackoff
		}
		if err == nil {
			backoff = streamMinBackoff
		}
	}
}

func (w *Watcher) stopPod(podUID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	prefix := podUID + "/"
	for key, cancel := range w.cancels {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			cancel()
		}
	}
}

func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, cancel := range w.cancels {
		cancel()
	}
}
