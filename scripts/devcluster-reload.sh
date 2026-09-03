#!/usr/bin/env bash
# Rebuilds all dev cluster images and restarts the workloads in place —
# use this after editing tracer or the log generators, without tearing the
# minikube cluster down and losing accumulated demo data.
set -euo pipefail
cd "$(dirname "$0")/.."

PROFILE=tracer-dev

echo "==> Rebuilding images"
docker build -f deploy/docker/Dockerfile.collector -t tracer-collector:dev .
docker build -f deploy/docker/Dockerfile.agent -t tracer-agent:dev .
docker build -f test/devcluster/httpgen/Dockerfile -t httpgen:dev .
docker build -f test/devcluster/pipelinegen/Dockerfile -t pipelinegen:dev .

echo "==> Reloading images into minikube"
minikube image load tracer-collector:dev -p "$PROFILE"
minikube image load tracer-agent:dev -p "$PROFILE"
minikube image load httpgen:dev -p "$PROFILE"
minikube image load pipelinegen:dev -p "$PROFILE"

echo "==> Restarting workloads to pick up new images"
kubectl --context "$PROFILE" -n tracer-system rollout restart deployment/tracer-collector
kubectl --context "$PROFILE" -n tracer-system rollout restart daemonset/tracer-agent
kubectl --context "$PROFILE" -n demo rollout restart deployment/gateway deployment/orders deployment/payments deployment/pipelinegen

kubectl --context "$PROFILE" -n tracer-system rollout status deployment/tracer-collector --timeout=180s
kubectl --context "$PROFILE" -n tracer-system rollout status daemonset/tracer-agent --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/gateway --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/orders --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/payments --timeout=180s
kubectl --context "$PROFILE" -n demo rollout status deployment/pipelinegen --timeout=180s

echo "==> Reload complete."
