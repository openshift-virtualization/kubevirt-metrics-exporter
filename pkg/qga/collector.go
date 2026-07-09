package qga

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift-virtualization/kubevirt-metrics-exporter/pkg/qmp"
)

type CollectorConfig struct {
	NodeName     string
	PollInterval time.Duration
	QGATimeout   int32
	ExecWait     time.Duration
	MaxRetries   int
	Concurrency  int
	Namespaces   []string
	LabelFilter  string
}

type vmState struct {
	mu           sync.Mutex
	client       *qmp.Client
	namespace    string
	vmi          string
	podName      string
	prevSnapshot map[string]DiskCounters
	retryCount   int
	stopped      bool
	stopReason   string
	closed       bool
}

func (vs *vmState) close() {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if !vs.closed {
		vs.closed = true
		vs.client.Close()
	}
}

type vmiResult struct {
	Namespace string
	VMI       string
	Node      string
	Disks     []DiskMetrics
}

type Collector struct {
	cfg       CollectorConfig
	podStore  cache.Store
	criClient *qmp.CRIClient
	log       *slog.Logger

	mu           sync.RWMutex
	results      []vmiResult
	scrapeErrors float64
	lastPollTS   float64

	connMu sync.RWMutex
	vms    map[string]*vmState
}

func NewCollector(cfg CollectorConfig, podStore cache.Store, criClient *qmp.CRIClient, log *slog.Logger) *Collector {
	return &Collector{
		cfg:       cfg,
		podStore:  podStore,
		criClient: criClient,
		log:       log,
		vms:       make(map[string]*vmState),
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
	for id, vs := range c.vms {
		vs.close()
		delete(c.vms, id)
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- latencyAvgDesc
	ch <- iopsDesc
	ch <- qgaScrapeErrorsDesc
	ch <- qgaLastPollDesc
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ch <- prometheus.MustNewConstMetric(qgaScrapeErrorsDesc, prometheus.CounterValue, c.scrapeErrors)
	ch <- prometheus.MustNewConstMetric(qgaLastPollDesc, prometheus.GaugeValue, c.lastPollTS)

	for _, vmi := range c.results {
		for _, disk := range vmi.Disks {
			if disk.RdIOPS > 0 || disk.RdLatSec > 0 {
				ch <- prometheus.MustNewConstMetric(
					latencyAvgDesc, prometheus.GaugeValue, disk.RdLatSec,
					vmi.Namespace, vmi.VMI, vmi.Node, disk.Name, "read",
				)
				ch <- prometheus.MustNewConstMetric(
					iopsDesc, prometheus.GaugeValue, disk.RdIOPS,
					vmi.Namespace, vmi.VMI, vmi.Node, disk.Name, "read",
				)
			}
			if disk.WrIOPS > 0 || disk.WrLatSec > 0 {
				ch <- prometheus.MustNewConstMetric(
					latencyAvgDesc, prometheus.GaugeValue, disk.WrLatSec,
					vmi.Namespace, vmi.VMI, vmi.Node, disk.Name, "write",
				)
				ch <- prometheus.MustNewConstMetric(
					iopsDesc, prometheus.GaugeValue, disk.WrIOPS,
					vmi.Namespace, vmi.VMI, vmi.Node, disk.Name, "write",
				)
			}
		}
	}
}

func matchesLabelFilter(labels map[string]string, filter string) bool {
	for _, part := range strings.Split(filter, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if labels[kv[0]] != kv[1] {
			return false
		}
	}
	return true
}

type podInfo struct {
	namespace string
	podName   string
	vmiName   string
}

func (c *Collector) poll(ctx context.Context) {
	c.log.Info("qga: starting poll cycle")

	var allPods []podInfo
	nsFilter := make(map[string]bool, len(c.cfg.Namespaces))
	for _, ns := range c.cfg.Namespaces {
		nsFilter[ns] = true
	}

	for _, obj := range c.podStore.List() {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if pod.Labels["kubevirt.io"] != "virt-launcher" {
			continue
		}
		if c.cfg.LabelFilter != "" && !matchesLabelFilter(pod.Labels, c.cfg.LabelFilter) {
			continue
		}
		if len(nsFilter) > 0 && !nsFilter[pod.Namespace] {
			continue
		}
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

	c.log.Info("qga: found virt-launcher pods", "count", len(allPods))

	type target struct {
		podInfo
		containerID string
	}

	var targets []target
	for _, pod := range allPods {
		info, err := c.criClient.FindComputePID(ctx, pod.podName, pod.namespace)
		if err != nil {
			c.log.Warn("qga: finding compute container", "namespace", pod.namespace, "pod", pod.podName, "error", err)
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
	for id, vs := range c.vms {
		if !activeIDs[id] {
			c.log.Info("qga: removing departed VM", "vmi", vs.vmi, "namespace", vs.namespace)
			vs.close()
			delete(c.vms, id)
		}
	}
	existing := make(map[string]bool, len(c.vms))
	for id := range c.vms {
		existing[id] = true
	}
	c.connMu.Unlock()

	for _, t := range targets {
		if existing[t.containerID] {
			continue
		}

		info, err := c.criClient.FindComputePID(ctx, t.podName, t.namespace)
		if err != nil {
			c.log.Warn("qga: getting PID for new VM", "namespace", t.namespace, "vmi", t.vmiName, "error", err)
			continue
		}

		vs, err := c.connectVM(t.namespace, t.vmiName, t.podName, info.PID)
		if err != nil {
			c.log.Error("qga: connecting to VM", "namespace", t.namespace, "vmi", t.vmiName, "error", err)
			continue
		}

		c.connMu.Lock()
		if _, dup := c.vms[t.containerID]; !dup {
			c.vms[t.containerID] = vs
		} else {
			vs.close()
		}
		c.connMu.Unlock()
	}

	var (
		resultsMu    sync.Mutex
		results      []vmiResult
		scrapeErrors int
	)

	sem := make(chan struct{}, c.cfg.Concurrency)
	var wg sync.WaitGroup

	c.connMu.RLock()
	snapshot := make(map[string]*vmState, len(c.vms))
	for k, v := range c.vms {
		snapshot[k] = v
	}
	c.connMu.RUnlock()

	for containerID, vs := range snapshot {
		vs.mu.Lock()
		if vs.stopped || vs.closed {
			c.log.Debug("qga: skipping VM", "vmi", vs.vmi, "stopped", vs.stopped, "closed", vs.closed, "reason", vs.stopReason)
			vs.mu.Unlock()
			continue
		}
		vs.mu.Unlock()

		wg.Add(1)
		go func(containerID string, vs *vmState) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := c.scrapeVM(ctx, vs)

			resultsMu.Lock()
			defer resultsMu.Unlock()

			if err != nil {
				scrapeErrors++
				c.handleScrapeError(containerID, vs, err)
				return
			}
			if result != nil {
				results = append(results, *result)
			}
		}(containerID, vs)
	}

	wg.Wait()

	c.mu.Lock()
	c.results = results
	c.scrapeErrors += float64(scrapeErrors)
	c.lastPollTS = float64(time.Now().Unix())
	c.mu.Unlock()

	c.log.Info("qga: poll cycle complete", "vms_with_data", len(results), "errors", scrapeErrors)
}

func (c *Collector) handleScrapeError(containerID string, vs *vmState, err error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if errors.Is(err, ErrCommandBlacklisted) {
		vs.stopped = true
		vs.stopReason = "command blacklisted"
		c.log.Warn("qga: stopping collection, command blacklisted",
			"namespace", vs.namespace, "vmi", vs.vmi)
		return
	}

	vs.retryCount++
	c.log.Warn("qga: scrape error",
		"namespace", vs.namespace, "vmi", vs.vmi,
		"error", err, "retries", vs.retryCount, "max", c.cfg.MaxRetries)

	if vs.retryCount >= c.cfg.MaxRetries {
		vs.stopped = true
		vs.stopReason = fmt.Sprintf("max retries (%d) exceeded: %v", c.cfg.MaxRetries, err)
		c.log.Warn("qga: stopping collection, max retries exceeded",
			"namespace", vs.namespace, "vmi", vs.vmi, "last_error", err)
	}
}

func (c *Collector) scrapeVM(ctx context.Context, vs *vmState) (*vmiResult, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if vs.closed {
		return nil, fmt.Errorf("connection closed")
	}

	counters, err := CollectDiskCounters(ctx, vs.client, c.cfg.QGATimeout, c.cfg.ExecWait, c.log)
	if err != nil {
		return nil, err
	}

	currSnapshot := make(map[string]DiskCounters, len(counters))
	for _, dc := range counters {
		currSnapshot[dc.Name] = dc
		c.log.Debug("qga: raw counters",
			"vmi", vs.vmi, "disk", dc.Name,
			"rd_qlen", dc.RdQueueLen, "wr_qlen", dc.WrQueueLen,
			"rd_ops", dc.RdOps, "wr_ops", dc.WrOps,
			"ts", dc.Timestamp100ns)
	}

	var disks []DiskMetrics
	if vs.prevSnapshot == nil {
		c.log.Debug("qga: first snapshot, no previous data to diff", "vmi", vs.vmi, "disks", len(currSnapshot))
	}
	if vs.prevSnapshot != nil {
		for name, curr := range currSnapshot {
			prev, ok := vs.prevSnapshot[name]
			if !ok {
				continue
			}
			m := ComputeMetrics(prev, curr)
			if m.ElapsedSec > 0 {
				disks = append(disks, m)
				c.log.Debug("qga: computed metrics",
					"vmi", vs.vmi, "disk", m.Name,
					"rd_lat_ms", m.RdLatSec*1000, "rd_iops", m.RdIOPS,
					"wr_lat_ms", m.WrLatSec*1000, "wr_iops", m.WrIOPS,
					"elapsed_s", m.ElapsedSec)
			}
		}
	}

	vs.prevSnapshot = currSnapshot
	vs.retryCount = 0

	if len(disks) == 0 {
		return nil, nil
	}

	return &vmiResult{
		Namespace: vs.namespace,
		VMI:       vs.vmi,
		Node:      c.cfg.NodeName,
		Disks:     disks,
	}, nil
}

func (c *Collector) connectVM(ns, vmi, podName string, pid int) (*vmState, error) {
	sockPath := fmt.Sprintf("/proc/%d/root/run/libvirt/virtqemud-sock", pid)
	if _, err := os.Stat(sockPath); err != nil {
		return nil, fmt.Errorf("virtqemud socket not found at %s: %w", sockPath, err)
	}

	domainName := ns + "_" + vmi
	client, err := qmp.Dial(sockPath, domainName)
	if err != nil {
		return nil, fmt.Errorf("dialing for QGA %s: %w", domainName, err)
	}

	c.log.Info("qga: connected to VM", "namespace", ns, "vmi", vmi, "pid", pid)
	return &vmState{
		client:    client,
		namespace: ns,
		vmi:       vmi,
		podName:   podName,
	}, nil
}
