#!/bin/bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-storage-latency-e2e}"

echo "=== Deleting Kind cluster ==="
kind delete cluster --name "${CLUSTER_NAME}"
echo "=== Teardown complete ==="
