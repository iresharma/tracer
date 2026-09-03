#!/usr/bin/env bash
# Port-forwards the tracer UI to http://localhost:8080. Blocks; Ctrl+C to stop.
set -euo pipefail
PROFILE=tracer-dev
echo "Opening the tracer UI at http://localhost:8080 (Ctrl+C to stop)"
kubectl --context "$PROFILE" -n tracer-system port-forward svc/tracer-collector 8080:80
