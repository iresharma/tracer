#!/usr/bin/env bash
# Tears down the tracer-dev minikube cluster entirely (including its disk).
set -euo pipefail
PROFILE=tracer-dev
echo "==> Deleting minikube profile '$PROFILE'"
minikube delete -p "$PROFILE"
