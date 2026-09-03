#!/usr/bin/env bash
# Brings up a local minikube-based playground cluster with tracer (agent +
# collector) plus two self-driving log generators, so you can explore the
# UI without wiring up real services. Safe to re-run.
set -euo pipefail
cd "$(dirname "$0")/.."

PROFILE=tracer-dev

echo "==> Ensuring minikube cluster '$PROFILE' is running"
if ! minikube status -p "$PROFILE" >/dev/null 2>&1; then
  minikube start -p "$PROFILE" --cpus=4 --memory=4000
else
  echo "    already running"
fi

echo "==> Building images"
docker build -f deploy/docker/Dockerfile.collector -t tracer-collector:dev .
docker build -f deploy/docker/Dockerfile.agent -t tracer-agent:dev .
docker build -f test/devcluster/httpgen/Dockerfile -t httpgen:dev .
docker build -f test/devcluster/pipelinegen/Dockerfile -t pipelinegen:dev .

echo "==> Loading images into minikube (profile: $PROFILE)"
minikube image load tracer-collector:dev -p "$PROFILE"
minikube image load tracer-agent:dev -p "$PROFILE"
minikube image load httpgen:dev -p "$PROFILE"
minikube image load pipelinegen:dev -p "$PROFILE"

echo "==> Applying tracer manifests"
kubectl --context "$PROFILE" apply -f deploy/k8s/namespace.yaml
kubectl --context "$PROFILE" apply -f deploy/k8s/

echo "==> Applying demo log generator manifests"
kubectl --context "$PROFILE" apply -f test/devcluster/k8s/namespace.yaml
kubectl --context "$PROFILE" apply -f test/devcluster/k8s/

echo "==> Waiting for rollouts"
kubectl --context "$PROFILE" -n tracer-system rollout status deployment/tracer-collector --timeout=180s
kubectl --context "$PROFILE" -n tracer-system rollout status daemonset/tracer-agent --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/payments --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/orders --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/gateway --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/pipelinegen --timeout=180s

cat <<EOF

==> Dev cluster is up.

Traffic is flowing already (gateway/orders/payments self-drive HTTP calls,
pipelinegen runs continuous pipeline executions) — give it 10-20s to
accumulate a few traces, then open the UI:

  scripts/devcluster-ui.sh

or manually:

  kubectl --context $PROFILE -n tracer-system port-forward svc/tracer-collector 8080:80

then visit http://localhost:8080 — try /logs to browse, or paste a trace_id
from any pod's logs into the trace lookup box on the dashboard.

Made a code change? Rebuild and redeploy without recreating the cluster:
  scripts/devcluster-reload.sh

Done exploring?
  scripts/devcluster-down.sh
EOF
