package csi

import (
	"context"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift-virtualization/kubevirt-metrics-exporter/pkg/device"
)

type Collector struct {
	discoverers []Discoverer
	metrics     *Metrics
	pvcIndexer  cache.Indexer
	logger      *slog.Logger
	interval    time.Duration
}

func NewCollector(discoverers []Discoverer, metrics *Metrics, pvcIndexer cache.Indexer, interval time.Duration, logger *slog.Logger) *Collector {
	return &Collector{
		discoverers: discoverers,
		metrics:     metrics,
		pvcIndexer:  pvcIndexer,
		logger:      logger,
		interval:    interval,
	}
}

func (c *Collector) Run(ctx context.Context) {
	c.runDiscovery(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runDiscovery(ctx)
		}
	}
}

func (c *Collector) runDiscovery(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	volumes := make(map[string]VolumeDevice)
	totalErrors := 0
	totalSuccesses := 0

	for _, d := range c.discoverers {
		results, err := d.Discover(ctx)

		if err != nil {
			totalErrors++
			c.metrics.IncDiscoveryErrors(d.Name())
			c.logger.Warn("csi: discovery error", "discoverer", d.Name(), "error", err)
			continue
		}

		totalSuccesses++
		for _, v := range results {
			if v.VolumeHandle == "" || v.Device == "" {
				continue
			}
			if existing, exists := volumes[v.VolumeHandle]; exists {
				if existing.Device != v.Device {
					c.logger.Warn("csi: conflicting device for volume_handle, keeping first",
						"volume_handle", v.VolumeHandle,
						"kept_device", existing.Device,
						"kept_discoverer", existing.Driver,
						"ignored_device", v.Device,
						"ignored_discoverer", v.Driver,
					)
				}
				continue
			}
			volumes[v.VolumeHandle] = v
		}
	}

	if totalSuccesses > 0 {
		c.resolvePVCs(volumes)
		c.metrics.Reconcile(volumes)
		c.metrics.SetLastSuccessfulNow()
	} else {
		c.logger.Error("csi: all discoverers failed, skipping reconcile",
			"discoverer_count", len(c.discoverers))
	}

	c.logger.Debug("csi: discovery cycle complete",
		"volumes_found", len(volumes), "errors", totalErrors, "successes", totalSuccesses)
}

func (c *Collector) resolvePVCs(volumes map[string]VolumeDevice) {
	if c.pvcIndexer == nil {
		return
	}
	for key, v := range volumes {
		if v.PVName == "" {
			continue
		}
		items, err := c.pvcIndexer.ByIndex(device.PVCByPVIndexName, v.PVName)
		if err != nil || len(items) == 0 {
			continue
		}
		pvc, ok := items[0].(*corev1.PersistentVolumeClaim)
		if !ok {
			continue
		}
		v.PVCName = pvc.Name
		v.Namespace = pvc.Namespace
		volumes[key] = v
	}
}
