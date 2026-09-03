package podwatcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iresharma/tracer/internal/agent/metrics"
)

// maxLineBytes caps a single log line the same way the hostPath tailer
// does — defense against a pathological single line consuming unbounded
// memory, not an expected case (the kubelet/CRI runtime themselves already
// split abnormally long single writes upstream of this).
const maxLineBytes = 256 * 1024

// Line is one complete log line sourced via the pods/log API rather than
// node-local file tailing.
type Line struct {
	Namespace string
	Pod       string
	PodUID    string
	Container string
	NodeName  string
	Timestamp time.Time
	// Stream is always "stdout": the pods/log API interleaves stdout and
	// stderr into a single stream with no way to tell them apart — a
	// known, long-standing limitation of this Kubernetes API, not
	// something this package can work around.
	Stream  string
	Content string
}

// streamContainerLogs opens a follow=true, timestamps=true log stream for
// one container and emits each line on out until ctx is cancelled or the
// stream ends (container terminated, connection dropped, etc — the caller
// decides whether/how to reconnect).
func streamContainerLogs(ctx context.Context, client *http.Client, cfg *InClusterConfig, ns, pod, podUID, nodeName, container string, out chan<- Line) error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?%s", ns, pod, url.Values{
		"container":  {container},
		"follow":     {"true"},
		"timestamps": {"true"},
	}.Encode())

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
		return fmt.Errorf("unexpected status %d streaming logs for %s/%s/%s", resp.StatusCode, ns, pod, container)
	}

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		raw, err := reader.ReadString('\n')
		if raw != "" {
			ts, content := splitTimestampedLine(raw)
			if len(content) > maxLineBytes {
				content = content[len(content)-maxLineBytes:]
			}
			line := Line{
				Namespace: ns,
				Pod:       pod,
				PodUID:    podUID,
				Container: container,
				NodeName:  nodeName,
				Timestamp: ts,
				Stream:    "stdout",
				Content:   content,
			}
			select {
			case out <- line:
				metrics.PodwatcherLinesTotal.Inc()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// splitTimestampedLine parses a line the way the kubelet's timestamps=true
// option formats it: "<RFC3339Nano timestamp> <content>\n". Falls back to
// the current time if a line doesn't have a well-formed timestamp prefix
// (should not happen in practice — the kubelet always writes this
// format — but a log-shipping path degrading to "wrong timestamp" instead
// of "dropped line" on any parsing surprise is the safer failure mode).
func splitTimestampedLine(raw string) (time.Time, string) {
	line := strings.TrimSuffix(raw, "\n")
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return time.Now().UTC(), line
	}
	ts, err := time.Parse(time.RFC3339Nano, line[:sp])
	if err != nil {
		return time.Now().UTC(), line
	}
	return ts, line[sp+1:]
}
