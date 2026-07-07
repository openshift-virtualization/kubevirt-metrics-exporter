package csi

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	volumeDeviceInfo  *prometheus.GaugeVec
	discoveryErrors   *prometheus.CounterVec
	volumesDiscovered *prometheus.GaugeVec
	lastSuccessful    prometheus.Gauge

	registry *prometheus.Registry

	mu              sync.Mutex
	previous        map[string]prometheus.Labels
	previousDrivers map[string]struct{}
}

func NewMetrics() *Metrics {
	return newMetricsWithRegistry(nil)
}

func newMetricsWithRegistry(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		volumeDeviceInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "csi_volume_node_device_info",
			Help: "Maps CSI volumes to node block devices. Value is always 1.",
		}, []string{"node", "volume_handle", "driver", "device", "namespace", "persistentvolumeclaim"}),

		discoveryErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "csi_volume_device_discovery_errors_total",
			Help: "Total number of discovery errors by discoverer.",
		}, []string{"discoverer"}),

		volumesDiscovered: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "csi_volume_device_volumes_discovered",
			Help: "Number of volumes discovered per driver.",
		}, []string{"driver"}),

		lastSuccessful: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "csi_volume_device_last_discovery_timestamp_seconds",
			Help: "Unix timestamp of last successful discovery cycle.",
		}),

		registry:        reg,
		previous:        make(map[string]prometheus.Labels),
		previousDrivers: make(map[string]struct{}),
	}

	if reg != nil {
		reg.MustRegister(m.volumeDeviceInfo)
		reg.MustRegister(m.discoveryErrors)
		reg.MustRegister(m.volumesDiscovered)
		reg.MustRegister(m.lastSuccessful)
	}

	return m
}

func (m *Metrics) Register() {
	prometheus.MustRegister(m.volumeDeviceInfo)
	prometheus.MustRegister(m.discoveryErrors)
	prometheus.MustRegister(m.volumesDiscovered)
	prometheus.MustRegister(m.lastSuccessful)
}

func (m *Metrics) IncDiscoveryErrors(discoverer string) {
	m.discoveryErrors.WithLabelValues(discoverer).Inc()
}

func (m *Metrics) SetLastSuccessfulNow() {
	m.lastSuccessful.SetToCurrentTime()
}

func (m *Metrics) Reconcile(volumes map[string]VolumeDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := make(map[string]prometheus.Labels, len(volumes))

	for key, v := range volumes {
		labels := prometheus.Labels{
			"node":                  v.Node,
			"volume_handle":         v.VolumeHandle,
			"driver":                v.Driver,
			"device":                v.Device,
			"namespace":             v.Namespace,
			"persistentvolumeclaim": v.PVCName,
		}
		m.volumeDeviceInfo.With(labels).Set(1)
		current[key] = labels
	}

	for key, labels := range m.previous {
		if _, exists := current[key]; !exists {
			m.volumeDeviceInfo.Delete(labels)
		}
	}

	m.previous = current

	driverCounts := make(map[string]float64)
	for _, v := range volumes {
		driverCounts[v.Driver]++
	}

	for driver := range m.previousDrivers {
		if _, exists := driverCounts[driver]; !exists {
			m.volumesDiscovered.Delete(prometheus.Labels{"driver": driver})
		}
	}

	currentDrivers := make(map[string]struct{}, len(driverCounts))
	for driver, count := range driverCounts {
		m.volumesDiscovered.With(prometheus.Labels{"driver": driver}).Set(count)
		currentDrivers[driver] = struct{}{}
	}
	m.previousDrivers = currentDrivers
}
