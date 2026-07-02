package qmp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	latencyDesc = prometheus.NewDesc(
		"kubevirt_storage_qmp_io_latency_seconds",
		"Block I/O latency histogram for KubeVirt VMI disks via QMP",
		[]string{"namespace", "vmi", "node", "drive", "operation"},
		nil,
	)

	scrapeErrorsDesc = prometheus.NewDesc(
		"kubevirt_storage_scrape_errors_total",
		"Total number of errors encountered during QMP scrape cycles",
		nil, nil,
	)

	lastPollDesc = prometheus.NewDesc(
		"kubevirt_storage_last_poll_timestamp_seconds",
		"Unix timestamp of the last successful QMP poll cycle",
		nil, nil,
	)
)

type VMIResult struct {
	Namespace string
	VMI       string
	Node      string
	Devices   []DeviceResult
}

type DeviceResult struct {
	DiskAlias string
	Stats     BlockStats
}

type PollerConfig struct {
	NodeName     string
	PollInterval time.Duration
	BoundariesNs []int64
	QMPTimeout   time.Duration
	Concurrency  int
	Namespaces   []string
	LabelFilter  string
}

type vmConnection struct {
	mu        sync.Mutex
	client    *Client
	namespace string
	vmi       string
	podName   string
	armed     map[string]bool
	closed    bool
}

func (vc *vmConnection) close() {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if !vc.closed {
		vc.closed = true
		vc.client.Close()
	}
}

type Collector struct {
	cfg       PollerConfig
	clientset kubernetes.Interface
	criClient *CRIClient
	log       *slog.Logger

	mu           sync.RWMutex
	results      []VMIResult
	scrapeErrors float64
	lastPollTS   float64

	connMu      sync.RWMutex
	connections map[string]*vmConnection
}

func NewCollector(cfg PollerConfig, cs kubernetes.Interface, criClient *CRIClient, log *slog.Logger) *Collector {
	return &Collector{
		cfg:         cfg,
		clientset:   cs,
		criClient:   criClient,
		log:         log,
		connections: make(map[string]*vmConnection),
	}
}

func (c *Collector) Run(ctx context.Context) {
	c.poll(ctx)
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.closeAll()
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

func (c *Collector) closeAll() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	for id, conn := range c.connections {
		conn.close()
		delete(c.connections, id)
	}
}

// Update replaces the cached poll results. Exported for testing.
func (c *Collector) Update(results []VMIResult, scrapeErrors int, lastPollTS float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = results
	c.scrapeErrors += float64(scrapeErrors)
	c.lastPollTS = lastPollTS
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- latencyDesc
	ch <- scrapeErrorsDesc
	ch <- lastPollDesc
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ch <- prometheus.MustNewConstMetric(scrapeErrorsDesc, prometheus.CounterValue, c.scrapeErrors)
	ch <- prometheus.MustNewConstMetric(lastPollDesc, prometheus.GaugeValue, c.lastPollTS)

	for _, vmi := range c.results {
		for _, dev := range vmi.Devices {
			type opData struct {
				hist    *LatencyHist
				totalNs uint64
			}
			operations := map[string]opData{
				"read":  {dev.Stats.RdLatencyHistogram, dev.Stats.RdTotalTimeNs},
				"write": {dev.Stats.WrLatencyHistogram, dev.Stats.WrTotalTimeNs},
				"flush": {dev.Stats.FlushLatencyHistogram, dev.Stats.FlushTotalTimeNs},
			}
			for op, data := range operations {
				if data.hist == nil {
					continue
				}
				buckets, count := ConvertBuckets(data.hist)
				if count == 0 {
					continue
				}
				sum := float64(data.totalNs) / 1e9
				h, err := prometheus.NewConstHistogram(
					latencyDesc,
					count, sum, buckets,
					vmi.Namespace, vmi.VMI, vmi.Node, dev.DiskAlias, op,
				)
				if err != nil {
					continue
				}
				ch <- h
			}
		}
	}
}

func ConvertBuckets(hist *LatencyHist) (map[float64]uint64, uint64) {
	buckets := make(map[float64]uint64, len(hist.Boundaries))
	var cumulative uint64

	for i, count := range hist.Bins {
		cumulative += count
		if i < len(hist.Boundaries) {
			buckets[hist.Boundaries[i]/1e9] = cumulative
		}
	}

	return buckets, cumulative
}

func (c *Collector) buildLabelSelector() string {
	sel := "kubevirt.io=virt-launcher"
	if c.cfg.LabelFilter != "" {
		sel += "," + c.cfg.LabelFilter
	}
	return sel
}

func (c *Collector) poll(ctx context.Context) {
	c.log.Info("qmp: starting poll cycle")

	namespaces := c.cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}

	type podInfo struct {
		namespace string
		podName   string
		vmiName   string
	}

	var allPods []podInfo
	labelSelector := c.buildLabelSelector()

	for _, ns := range namespaces {
		pods, err := c.clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
			FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", c.cfg.NodeName),
		})
		if err != nil {
			c.log.Error("qmp: listing pods", "namespace", ns, "error", err)
			continue
		}
		for _, pod := range pods.Items {
			vmiName := pod.Labels["vm.kubevirt.io/name"]
			if vmiName == "" {
				continue
			}
			allPods = append(allPods, podInfo{
				namespace: pod.Namespace,
				podName:   pod.Name,
				vmiName:   vmiName,
			})
		}
	}

	c.log.Info("qmp: found virt-launcher pods", "count", len(allPods))

	type target struct {
		podInfo
		containerID string
	}

	var targets []target
	for _, pod := range allPods {
		info, err := c.criClient.FindComputePID(ctx, pod.podName, pod.namespace)
		if err != nil {
			c.log.Warn("qmp: finding compute container", "namespace", pod.namespace, "pod", pod.podName, "error", err)
			continue
		}
		targets = append(targets, target{
			podInfo:     pod,
			containerID: info.ContainerID,
		})
	}

	activeIDs := make(map[string]bool, len(targets))
	for _, t := range targets {
		activeIDs[t.containerID] = true
	}

	c.connMu.Lock()
	for id, conn := range c.connections {
		if !activeIDs[id] {
			c.log.Info("qmp: removing departed VM", "vmi", conn.vmi, "namespace", conn.namespace)
			conn.close()
			delete(c.connections, id)
		}
	}
	existing := make(map[string]bool, len(c.connections))
	for id := range c.connections {
		existing[id] = true
	}
	c.connMu.Unlock()

	for _, t := range targets {
		if existing[t.containerID] {
			continue
		}

		info, err := c.criClient.FindComputePID(ctx, t.podName, t.namespace)
		if err != nil {
			c.log.Warn("qmp: getting PID for new VM", "namespace", t.namespace, "vmi", t.vmiName, "error", err)
			continue
		}

		conn, err := c.connectVM(t.namespace, t.vmiName, t.podName, info.PID)
		if err != nil {
			c.log.Error("qmp: connecting to VM", "namespace", t.namespace, "vmi", t.vmiName, "error", err)
			continue
		}

		c.connMu.Lock()
		if _, dup := c.connections[t.containerID]; !dup {
			c.connections[t.containerID] = conn
		} else {
			conn.close()
		}
		c.connMu.Unlock()
	}

	var (
		resultsMu    sync.Mutex
		results      []VMIResult
		scrapeErrors int
	)

	sem := make(chan struct{}, c.cfg.Concurrency)
	var wg sync.WaitGroup

	c.connMu.RLock()
	snapshot := make(map[string]*vmConnection, len(c.connections))
	for k, v := range c.connections {
		snapshot[k] = v
	}
	c.connMu.RUnlock()

	for containerID, conn := range snapshot {
		wg.Add(1)
		go func(containerID string, conn *vmConnection) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := c.scrapeVM(ctx, conn)
			resultsMu.Lock()
			defer resultsMu.Unlock()
			if err != nil {
				c.log.Error("qmp: scraping VM", "namespace", conn.namespace, "vmi", conn.vmi, "error", err)
				scrapeErrors++
				conn.close()
				c.connMu.Lock()
				delete(c.connections, containerID)
				c.connMu.Unlock()
				return
			}
			results = append(results, *result)
		}(containerID, conn)
	}

	wg.Wait()

	c.mu.Lock()
	c.results = results
	c.scrapeErrors += float64(scrapeErrors)
	c.lastPollTS = float64(time.Now().Unix())
	c.mu.Unlock()

	c.log.Info("qmp: poll cycle complete", "vms", len(results), "errors", scrapeErrors)
}

func (c *Collector) connectVM(ns, vmi, podName string, pid int) (*vmConnection, error) {
	sockPath := fmt.Sprintf("/proc/%d/root/run/libvirt/virtqemud-sock", pid)
	if _, err := os.Stat(sockPath); err != nil {
		return nil, fmt.Errorf("virtqemud socket not found at %s: %w", sockPath, err)
	}

	domainName := ns + "_" + vmi
	client, err := Dial(sockPath, domainName)
	if err != nil {
		return nil, fmt.Errorf("dialing QMP for %s: %w", domainName, err)
	}

	c.log.Info("qmp: connected to VM", "namespace", ns, "vmi", vmi, "pid", pid)
	return &vmConnection{
		client:    client,
		namespace: ns,
		vmi:       vmi,
		podName:   podName,
		armed:     make(map[string]bool),
	}, nil
}

func (c *Collector) scrapeVM(ctx context.Context, conn *vmConnection) (*VMIResult, error) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return nil, fmt.Errorf("connection closed")
	}

	qmpCtx, cancel := context.WithTimeout(ctx, c.cfg.QMPTimeout)
	defer cancel()

	resp, err := conn.client.QueryBlockStats(qmpCtx)
	if err != nil {
		return nil, err
	}

	for i := range resp.Return {
		dev := &resp.Return[i]
		_, deviceID, ok := ExtractDiskInfo(dev.EffectiveQDev())
		if !ok {
			continue
		}
		if conn.armed[deviceID] {
			continue
		}
		if !HasHistograms(dev) {
			c.log.Info("qmp: enabling histogram", "vmi", conn.vmi, "device_id", deviceID)
			armCtx, armCancel := context.WithTimeout(ctx, c.cfg.QMPTimeout)
			err := conn.client.EnableHistogram(armCtx, deviceID, c.cfg.BoundariesNs)
			armCancel()
			if err != nil {
				c.log.Warn("qmp: failed to arm histogram", "vmi", conn.vmi, "device_id", deviceID, "error", err)
				continue
			}
		}
		conn.armed[deviceID] = true
	}

	var devices []DeviceResult
	for i := range resp.Return {
		dev := &resp.Return[i]
		alias, _, ok := ExtractDiskInfo(dev.EffectiveQDev())
		if !ok {
			continue
		}
		if !HasHistograms(dev) {
			continue
		}
		devices = append(devices, DeviceResult{
			DiskAlias: alias,
			Stats:     dev.Stats,
		})
	}

	return &VMIResult{
		Namespace: conn.namespace,
		VMI:       conn.vmi,
		Node:      c.cfg.NodeName,
		Devices:   devices,
	}, nil
}
