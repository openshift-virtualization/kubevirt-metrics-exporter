package ebpf

import (
	"log/slog"

	ciliumebpf "github.com/cilium/ebpf"
	"github.com/openshift-virtualization/kubevirt-storage-latency-exporter/pkg/device"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	podBlockDesc = prometheus.NewDesc(
		"kubevirt_storage_block_io_latency_seconds",
		"Histogram of block I/O latency in seconds, attributed to a pod volume",
		[]string{"node", "persistentvolume", "pod_uid", "operation"},
		nil,
	)
	systemBlockDesc = prometheus.NewDesc(
		"kubevirt_storage_system_block_io_latency_seconds",
		"Histogram of block I/O latency in seconds for system/unresolvable devices",
		[]string{"node", "device", "operation"},
		nil,
	)
	nfsDesc = prometheus.NewDesc(
		"kubevirt_storage_nfs_io_latency_seconds",
		"Histogram of NFS I/O latency in seconds",
		[]string{"node", "persistentvolume", "pod_uid", "operation"},
		nil,
	)
	nfsVfsDesc = prometheus.NewDesc(
		"kubevirt_storage_nfs_vfs_latency_seconds",
		"Histogram of NFS VFS call latency in seconds (kprobe-based)",
		[]string{"node", "persistentvolume", "pod_uid", "operation"},
		nil,
	)
	subsystemDesc = prometheus.NewDesc(
		"kubevirt_storage_subsystem_active",
		"Whether an eBPF monitoring subsystem is active (1) or failed to load (0)",
		[]string{"subsystem"},
		nil,
	)

	opLabels       = [4]string{"read", "write", "discard", "flush"}
	nfsVfsOpLabels = [4]string{"read", "write", "open", "getattr"}
)

type Collector struct {
	blockHists      *ciliumebpf.Map
	nfsHists        *ciliumebpf.Map
	nfsKprobeHists  *ciliumebpf.Map
	resolver        *device.Resolver
	nodeName        string
	buckets         []float64
	blockActive     bool
	nfsActive       bool
	nfsKprobeActive bool
	log             *slog.Logger
}

func NewCollector(programs *Programs, resolver *device.Resolver, nodeName string, buckets []float64, log *slog.Logger) *Collector {
	return &Collector{
		blockHists:      programs.BlockHists,
		nfsHists:        programs.NfsHists,
		nfsKprobeHists:  programs.NfsKprobeHists,
		blockActive:     programs.BlockActive,
		nfsActive:       programs.NFSActive,
		nfsKprobeActive: programs.NFSKprobeActive,
		resolver:        resolver,
		nodeName:        nodeName,
		buckets:         buckets,
		log:             log,
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- podBlockDesc
	ch <- systemBlockDesc
	ch <- nfsDesc
	ch <- nfsVfsDesc
	ch <- subsystemDesc
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.collectSubsystemGauge(ch)
	if c.blockHists != nil {
		c.collectBlock(ch)
	}
	if c.nfsHists != nil {
		c.collectNFS(ch)
	}
	if c.nfsKprobeHists != nil {
		c.collectNFSKprobe(ch)
	}
}

func (c *Collector) collectSubsystemGauge(ch chan<- prometheus.Metric) {
	val := 0.0
	if c.blockActive {
		val = 1.0
	}
	ch <- prometheus.MustNewConstMetric(subsystemDesc, prometheus.GaugeValue, val, "block")

	val = 0.0
	if c.nfsActive {
		val = 1.0
	}
	ch <- prometheus.MustNewConstMetric(subsystemDesc, prometheus.GaugeValue, val, "nfs")

	val = 0.0
	if c.nfsKprobeActive {
		val = 1.0
	}
	ch <- prometheus.MustNewConstMetric(subsystemDesc, prometheus.GaugeValue, val, "nfs_kprobe")
}

type blockHistKey struct {
	Dev uint32
	Op  uint8
	Pad [3]uint8
}

type nfsHistKey struct {
	Dev uint32
	Op  uint8
	Pad [3]uint8
}

type histValue struct {
	Slots [MaxSlots]uint64
}

func (c *Collector) collectBlock(ch chan<- prometheus.Metric) {
	var key blockHistKey
	var val histValue
	iter := c.blockHists.Iterate()

	for iter.Next(&key, &val) {
		if key.Op > 3 {
			continue
		}
		count, sum, buckets := SlotsToConstHistogram(val.Slots, c.buckets)
		if count == 0 {
			continue
		}

		info, resolved := c.resolver.Lookup(key.Dev)
		if resolved {
			m, err := prometheus.NewConstHistogram(
				podBlockDesc,
				count, sum, buckets,
				c.nodeName, info.PVName, info.PodUID, opLabels[key.Op],
			)
			if err != nil {
				c.log.Error("creating block histogram metric", "error", err)
				continue
			}
			ch <- m
		} else {
			devName := device.DevToString(key.Dev)
			m, err := prometheus.NewConstHistogram(
				systemBlockDesc,
				count, sum, buckets,
				c.nodeName, devName, opLabels[key.Op],
			)
			if err != nil {
				c.log.Error("creating system block histogram metric", "error", err)
				continue
			}
			ch <- m
		}
	}

	if err := iter.Err(); err != nil {
		c.log.Error("iterating block histogram map", "error", err)
	}
}

func (c *Collector) collectNFS(ch chan<- prometheus.Metric) {
	var key nfsHistKey
	var val histValue
	iter := c.nfsHists.Iterate()

	for iter.Next(&key, &val) {
		if key.Op > 1 {
			continue
		}
		count, sum, buckets := SlotsToConstHistogram(val.Slots, c.buckets)
		if count == 0 {
			continue
		}

		info, resolved := c.resolver.Lookup(key.Dev)
		pv := ""
		podUID := ""
		if resolved {
			pv = info.PVName
			podUID = info.PodUID
		}

		m, err := prometheus.NewConstHistogram(
			nfsDesc,
			count, sum, buckets,
			c.nodeName, pv, podUID, opLabels[key.Op],
		)
		if err != nil {
			c.log.Error("creating NFS histogram metric", "error", err)
			continue
		}
		ch <- m
	}

	if err := iter.Err(); err != nil {
		c.log.Error("iterating NFS histogram map", "error", err)
	}
}

func (c *Collector) collectNFSKprobe(ch chan<- prometheus.Metric) {
	var key nfsHistKey
	var val histValue
	iter := c.nfsKprobeHists.Iterate()

	for iter.Next(&key, &val) {
		if key.Op > 3 {
			continue
		}
		count, sum, buckets := SlotsToConstHistogram(val.Slots, c.buckets)
		if count == 0 {
			continue
		}

		info, resolved := c.resolver.Lookup(key.Dev)
		pv := ""
		podUID := ""
		if resolved {
			pv = info.PVName
			podUID = info.PodUID
		}

		m, err := prometheus.NewConstHistogram(
			nfsVfsDesc,
			count, sum, buckets,
			c.nodeName, pv, podUID, nfsVfsOpLabels[key.Op],
		)
		if err != nil {
			c.log.Error("creating NFS VFS histogram metric", "error", err)
			continue
		}
		ch <- m
	}

	if err := iter.Err(); err != nil {
		c.log.Error("iterating NFS kprobe histogram map", "error", err)
	}
}
