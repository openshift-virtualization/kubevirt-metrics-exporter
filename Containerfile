FROM quay.io/centos/centos:stream9 AS builder

RUN dnf install -y --enablerepo=crb \
    clang \
    llvm \
    libbpf-devel \
    elfutils-libelf-devel \
    zlib-devel \
    make \
    gcc \
    golang \
    bpftool \
    && dnf clean all

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go generate ./pkg/ebpf/...

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /kubevirt-metrics-exporter ./cmd/exporter/

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

COPY --from=builder /kubevirt-metrics-exporter /usr/local/bin/kubevirt-metrics-exporter

LABEL name="kubevirt-metrics-exporter" \
      summary="Storage I/O latency exporter for OpenShift Virtualization" \
      description="Monitors VM and host storage I/O latency using QMP and eBPF, exports Prometheus metrics" \
      io.k8s.display-name="KubeVirt Storage Latency Exporter" \
      io.openshift.tags="kubevirt,monitoring,storage,ebpf"

USER 0
ENTRYPOINT ["/usr/local/bin/kubevirt-metrics-exporter"]
