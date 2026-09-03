// Package podwatcher is an alternative log ingestion source to the
// hostPath tailer: it streams container logs via the Kubernetes pods/log
// API (the same mechanism `kubectl logs -f` uses) instead of reading
// node-local files. This needs no hostPath — a plain Deployment using it
// passes the "restricted" and "baseline" Pod Security Standards without
// exception — at the cost of needing RBAC (get/list/watch pods, get
// pods/log) and only ever seeing pods in one namespace.
//
// Deliberately hand-rolled against the raw REST API rather than pulling in
// k8s.io/client-go: tracer's whole ethos is a minimal dependency tree
// (see the agent's existing from-scratch CRI parser, tailer, and HTTP
// forwarder), and this package only ever needs three endpoints — LIST and
// WATCH pods, and a single log-streaming GET — which is a small enough
// surface that client-go's generated clientset/informer machinery (and
// its large transitive dependency graph) isn't worth it here.
package podwatcher

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Package-level vars, not consts: tests override these to point at a
// temp-dir fixture instead of the real serviceaccount volume, which won't
// exist (and shouldn't be written to) outside a real pod.
var (
	tokenPath     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caCertPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	namespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// InClusterConfig holds what's needed to call the API server from inside a
// pod. The token is deliberately *not* cached here — kubelet rotates the
// projected serviceaccount token on disk well before it expires (default
// TTL is around an hour), so it's re-read fresh on every request in
// NewRequest rather than captured once at startup, which would eventually
// start failing auth on a long-running agent.
type InClusterConfig struct {
	BaseURL          string // e.g. https://10.96.0.1:443
	DefaultNamespace string // the pod's own namespace; a fallback, not a restriction
}

// LoadInClusterConfig reads the standard in-cluster serviceaccount files.
// Returns an error if any of them are missing — which just means the pod
// wasn't given a serviceaccount token (or automountServiceAccountToken is
// false), the same failure mode client-go's InClusterConfig() has.
func LoadInClusterConfig() (*InClusterConfig, error) {
	if _, err := os.Stat(tokenPath); err != nil {
		return nil, fmt.Errorf("stat serviceaccount token: %w", err)
	}
	nsBytes, err := os.ReadFile(namespacePath)
	if err != nil {
		return nil, fmt.Errorf("read serviceaccount namespace: %w", err)
	}

	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST/PORT not set — not running in a pod?")
	}

	return &InClusterConfig{
		BaseURL:          fmt.Sprintf("https://%s:%s", host, port),
		DefaultNamespace: strings.TrimSpace(string(nsBytes)),
	}, nil
}

// HTTPClient builds an *http.Client trusting the cluster's CA (read from
// the same serviceaccount volume) and long enough timeouts to hold a
// follow=true log stream open indefinitely (Timeout is deliberately unset
// at the client level — per-request context deadlines are used instead by
// callers that need one, since a shared client-level timeout would kill
// long-lived watch/log streams).
func (c *InClusterConfig) HTTPClient() (*http.Client, error) {
	caBytes, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read serviceaccount CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("no valid certificates found in %s", caCertPath)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: pool,
		},
		// A watch/follow connection can legitimately sit idle for a while
		// between events; don't let the transport's own idle-conn reaping
		// interfere with that (this only bounds idle time of *pooled,
		// reusable* connections, not an active streaming read).
		IdleConnTimeout: 90 * time.Second,
	}
	return &http.Client{Transport: transport}, nil
}

// NewRequest builds a GET request against the API server with the bearer
// token attached (re-read from disk on every call — see the InClusterConfig
// doc comment). path must start with "/" (e.g. "/api/v1/namespaces/...").
func (c *InClusterConfig) NewRequest(path string) (*http.Request, error) {
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("read serviceaccount token: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenBytes)))
	return req, nil
}
