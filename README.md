# tracer

A minimal, lightweight log/trace collector for Kubernetes. Tails container
logs directly from the node, stitches log lines that share a `trace_id`
into a cross-service trace, and stores everything in day-partitioned
SQLite — no OpenTelemetry collector, no external database, no RBAC.
Built to run comfortably under 512MB RAM.

## Why

Full observability stacks (SigNoz, Jaeger + Tempo + Loki, Datadog) are
overkill for a small cluster: multiple StatefulSets, a real time-series
database, and a meaningful chunk of the cluster's own resource budget just
to watch the cluster. tracer trades away real distributed-tracing spans
(no OTel SDK integration, no span start/end/duration) for something you
can run on a homelab node: two static Go binaries, SQLite on a PVC, and a
correlation convention your apps already mostly follow — log structured
JSON with a `trace_id` field, and tracer does the rest.

## Architecture

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│  Pod logs    │      │  Pod logs    │      │  Pod logs    │
│ (any ns)     │      │ (any ns)     │      │ (any ns)     │
└──────┬───────┘      └──────┬───────┘      └──────┬───────┘
       │  /var/log (hostPath)                       │
┌──────▼──────────────────────▼──────────────────────▼──────┐
│              tracer-agent (DaemonSet, 1/node)               │
│  tails CRI log files, extracts trace_id from JSON logs,     │
│  batches + forwards over HTTP. No k8s API access, no RBAC.  │
└──────────────────────────┬──────────────────────────────────┘
                            │ POST /api/v1/logs
                  ┌─────────▼─────────┐
                  │  tracer-collector  │  SQLite (day-partitioned),
                  │  (Deployment, x1)  │  HTMX+Alpine UI, JSON API,
                  │                    │  read-only SQL query page
                  └────────────────────┘
```

- **Agent**: polls container log files (handles both containerd's
  plaintext CRI format and Docker's json-file format), reassembles
  runtime-split partial lines, derives namespace/pod/container purely from
  the kubelet's log path naming (no API server calls), checkpoints its
  read offset so restarts don't replay or drop lines, and forwards batches
  with a bounded, drop-oldest ring buffer if the collector is unreachable.
- **Collector**: SQLite storage with one table per UTC day
  (`logs_YYYYMMDD`), so the 10-day retention policy is a `DROP TABLE`
  instead of `DELETE + VACUUM`. Serves a JSON API, a server-rendered
  HTMX/Alpine.js UI (dashboard, log browser, trace waterfall, ad-hoc SQL
  query page), and enforces read-only SQL at the SQLite engine level (a
  separate connection pool with `PRAGMA query_only`, not just a string
  check) for the query page.

See `deploy/k8s/` for the manifests and their design notes (inline
comments explain the non-obvious choices — `Recreate` deploy strategy,
no agent RBAC, hostPath mount rationale, etc).

## Quickstart: local playground

Spins up a `minikube` cluster with tracer plus two self-driving log
generators (an HTTP service mesh and a CI/CD pipeline simulator) so there's
real, continuously-flowing data to explore in the UI — no manual curl
needed.

```sh
make devcluster-up      # first run: builds images, starts minikube, deploys everything
make devcluster-ui      # port-forwards the UI to http://localhost:8080
make devcluster-reload  # after code changes: rebuild + redeploy in place
make devcluster-down    # tear down
```

See `scripts/devcluster-*.sh` for what each step does.

## Development

```sh
make build   # go build ./...
make test    # go test ./...
make vet
make fmt
```

Standard Go project layout: `cmd/agent` and `cmd/collector` are the two
binaries, `internal/agent/*` and `internal/collector/*` hold their
respective logic, `internal/model` is the shared wire format between them.

## Deploying

Build and push both images (`deploy/docker/Dockerfile.agent`,
`Dockerfile.collector`), then apply `deploy/k8s/*.yaml` — adjust the
`image:` references, the collector's PVC size (sizing note is in
`collector-pvc.yaml`), and retention/config values in the two ConfigMaps
for your environment first.

## Status

Personal/homelab project — functional and covered by unit tests
(`go test ./...` across the store, parser, tailer, and network-facing
packages), but there's no authentication in front of the UI or API, so
don't expose the collector Service outside the cluster without putting
your own auth in front of it.
