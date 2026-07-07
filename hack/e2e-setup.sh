#!/bin/bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-storage-latency-e2e}"
IMAGE="localhost/kubevirt-metrics-exporter:e2e"
NAMESPACE="kubevirt-metrics-exporter"

echo "=== Creating Kind cluster ==="
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "Cluster ${CLUSTER_NAME} already exists"
else
    kind create cluster --name "${CLUSTER_NAME}" --wait 60s
fi

echo "=== Building exporter image ==="
if command -v docker &>/dev/null; then
    CONTAINER_TOOL="docker"
elif command -v podman &>/dev/null; then
    CONTAINER_TOOL="podman"
else
    echo "ERROR: neither docker nor podman found"
    exit 1
fi
${CONTAINER_TOOL} build -f Containerfile -t "${IMAGE}" .

echo "=== Loading image into Kind ==="
if [ "${CONTAINER_TOOL}" = "podman" ]; then
    podman save "${IMAGE}" | kind load image-archive /dev/stdin --name "${CLUSTER_NAME}"
else
    kind load docker-image "${IMAGE}" --name "${CLUSTER_NAME}"
fi

echo "=== Deploying exporter ==="
cd deploy/e2e
kustomize edit set image "quay.io/openshift-virtualization/kubevirt-metrics-exporter=${IMAGE}"
cd ../..
kubectl apply -k deploy/e2e/

echo "=== Waiting for DaemonSet rollout ==="
kubectl -n "${NAMESPACE}" rollout status daemonset/kubevirt-metrics-exporter --timeout=120s

echo "=== Setup complete ==="
kubectl -n "${NAMESPACE}" get pods
