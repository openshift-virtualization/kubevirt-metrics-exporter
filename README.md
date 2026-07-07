# KubeVirt Metrics Exporter

A Prometheus exporter for OpenShift Virtualization storage observability. It runs as a DaemonSet and combines three collection subsystems in a single container:

- **QMP subsystem** — connects to each VM's QEMU Monitor Protocol to collect per-disk read/write/flush latency histograms directly from the hypervisor
- **eBPF subsystem** — attaches kernel tracepoints and kprobes to capture block and NFS I/O latency across the node, correlated to Kubernetes pods and PersistentVolumeClaims
- **CSI discovery subsystem** — maps CSI volumes to their underlying node block devices, enabling alerts that correlate storage path health (multipath, NVMe-oF) with specific PersistentVolumes

All subsystems are independently enabled/disabled and degrade gracefully if one fails to start.

## Metrics

QMP and eBPF metrics use the `kubevirt_*` prefix. CSI discovery metrics use the `csi_volume_*` prefix.

### QMP metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_qmp_io_latency_seconds` | histogram | namespace, vmi, node, drive, operation, persistentvolumeclaim | Per-disk I/O latency for KubeVirt VMs |
| `kubevirt_qmp_scrape_errors_total` | counter | | Errors during QMP poll cycles |
| `kubevirt_qmp_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last QMP poll |

### eBPF metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_block_io_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | Block I/O latency attributed to pod volumes |
| `kubevirt_system_block_io_latency_seconds` | histogram | node, device, operation | Block I/O latency for system/unresolved devices |
| `kubevirt_nfs_io_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | NFS I/O latency (tracepoint-based) |
| `kubevirt_nfs_vfs_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | NFS VFS call latency (kprobe-based) |
| `kubevirt_subsystem_active` | gauge | subsystem | Whether an eBPF subsystem loaded successfully (1) or not (0) |

### CSI discovery metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `csi_volume_node_device_info` | gauge | node, volume_handle, driver, device, namespace, persistentvolumeclaim | Maps CSI volumes to node block devices (value always 1) |
| `csi_volume_device_discovery_errors_total` | counter | discoverer | Total discovery errors by discoverer |
| `csi_volume_device_volumes_discovered` | gauge | driver | Number of volumes discovered per CSI driver |
| `csi_volume_device_last_discovery_timestamp_seconds` | gauge | | Unix timestamp of last successful discovery cycle |

### Example PromQL

P99 write latency per VMI:
```promql
histogram_quantile(0.99,
  sum by (vmi, le) (
    rate(kubevirt_qmp_io_latency_seconds_bucket{operation="write"}[5m])
  )
)
```

## Alerts

The CSI discovery subsystem provides five Prometheus alerting rules for storage path health:

| Alert | Severity | Description |
|-------|----------|-------------|
| `CSIVolumeMultipathDegraded` | warning | A PV-backed multipath device has non-active paths |
| `CSIVolumeMultipathLost` | critical | All multipath paths are down, I/O is failing |
| `CSIVolumeNVMeSubsystemDegraded` | warning | An NVMe-oF subsystem has non-live controllers |
| `CSIVolumeNVMeSubsystemLost` | critical | All NVMe-oF controllers are dead |
| `CSIVolumeDeviceExporterDown` | warning | The exporter is not being scraped |

These alerts join `csi_volume_node_device_info` with `kube_persistentvolume_info` and node_exporter's `node_dmmultipath_*` / `node_nvmesubsystem_*` collectors.

Alert rules are defined in Go (`pkg/monitoring/rules/alerts/`) and generated as YAML:

```bash
make generate-rules    # regenerate pkg/monitoring/rules/alerts.yaml
make test-alerts       # lint and unit test alerts via promtool (requires podman)
```

Runbooks for each alert are in [`docs/runbooks/`](docs/runbooks/).

## Configuration

Shared flags apply to all subsystems. Subsystem-specific flags are prefixed with `--qmp-`, `--ebpf-`, or `--csi-`. All flags can be overridden via environment variables.

### Shared

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--listen-address` | `LISTEN_ADDRESS` | `:8080` | Metrics server listen address |
| `--log-level` | `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--boundaries` | `BOUNDARIES` | `10000000,100000000,1000000000` | Histogram bucket boundaries in nanoseconds |
| | `NODE_NAME` | (required) | Node name, typically from downward API |
| `--namespaces` | `NAMESPACES` | (all) | Comma-separated namespace filter (applies to QMP and eBPF) |

### QMP

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-qmp` | `ENABLE_QMP` | `true` | Enable QMP collection |
| `--qmp-poll-interval` | `QMP_POLL_INTERVAL` | `1m` | VM scrape interval |
| `--qmp-concurrency` | `QMP_CONCURRENCY` | `8` | Max parallel QMP operations |
| `--qmp-timeout` | `QMP_TIMEOUT` | `5s` | Per-operation QMP timeout |
| `--qmp-cri-socket` | `QMP_CRI_SOCKET` | `/run/crio/crio.sock` | CRI-O socket path |
| `--qmp-label-filter` | `QMP_LABEL_FILTER` | | Additional pod label selector |

### eBPF

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-ebpf` | `ENABLE_EBPF` | `true` | Enable eBPF collection |
| `--enable-ebpf-block` | `ENABLE_EBPF_BLOCK` | `true` | Enable block I/O tracing |
| `--enable-ebpf-nfs` | `ENABLE_EBPF_NFS` | `true` | Enable NFS tracing |
| `--enable-ebpf-nfs-kprobe` | `ENABLE_EBPF_NFS_KPROBE` | `false` | Enable NFS VFS kprobe tracing |
| `--ebpf-scan-interval` | `EBPF_SCAN_INTERVAL` | `30` | Device-to-pod resolution interval (seconds) |
| `--ebpf-proc-path` | `EBPF_PROC_PATH` | `/proc` | Host proc filesystem path |

### CSI discovery

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-csi` | `ENABLE_CSI` | `true` | Enable CSI volume-to-device discovery |
| `--csi-poll-interval` | `CSI_POLL_INTERVAL` | `30s` | Discovery poll interval |
| `--csi-kubelet-root` | `CSI_KUBELET_ROOT` | `/var/lib/kubelet` | Path to kubelet root (must have mountPropagation: HostToContainer) |
| `--csi-host-sys` | `CSI_HOST_SYS` | `/sys` | Path to host /sys inside container |
| `--csi-host-trident-tracking` | `CSI_HOST_TRIDENT_TRACKING` | `/host/trident/tracking` | Path to Trident tracking dir inside container |

## Building

Prerequisites: Go 1.25+, clang, llvm, libbpf-devel

```bash
make build            # generates eBPF bindings and builds the binary
make test             # runs all unit tests
make test-alerts      # lint and unit test alert rules (requires podman)
make image            # builds container image with podman
```

To build and push a custom image:

```bash
make push IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
```

## Deploying

### From a release

Download the install manifest from the [latest release](https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest):

OpenShift:

```bash
oc apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-openshift.yaml
```

Kubernetes:

```bash
kubectl apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-kubernetes.yaml
```

### From source

OpenShift:

```bash
make deploy
```

Kubernetes:

```bash
make deploy-kubernetes
```

To deploy with a custom image:

```bash
make deploy IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
```

The OpenShift variant includes SecurityContextConstraints, worker node selector, and PodMonitor for Prometheus scraping.

### Required capabilities

| Capability | Reason |
|-----------|--------|
| `hostPID` | Access VM virtqemud sockets via `/proc/<pid>/root/` |
| `SYS_PTRACE` | Traverse `/proc/<pid>/root/` of other containers |
| `DAC_OVERRIDE` | Connect to virtqemud socket owned by qemu UID |
| `BPF` | Load and attach eBPF programs |
| `PERFMON` | Attach to kernel tracepoints and kprobes |
| `SYS_RESOURCE` | Increase eBPF map memory limits |
