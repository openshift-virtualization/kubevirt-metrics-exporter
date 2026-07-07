package csi

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

var _ = Describe("Metrics", func() {
	var m *Metrics

	BeforeEach(func() {
		// Use a fresh registry per test to avoid cross-test pollution.
		reg := prometheus.NewPedanticRegistry()
		m = newMetricsWithRegistry(reg)
	})

	Describe("Reconcile", func() {
		It("should add new metrics", func() {
			volumes := map[string]VolumeDevice{
				"vol-1": {VolumeHandle: "vol-1", Driver: "csi.trident.netapp.io", Device: "dm-0", Node: "node1"},
				"vol-2": {VolumeHandle: "vol-2", Driver: "csi.hpe.com", Device: "dm-1", Node: "node1"},
			}
			m.Reconcile(volumes)

			count := testutil.CollectAndCount(m.volumeDeviceInfo)
			Expect(count).To(Equal(2))
		})

		It("should remove stale series", func() {
			volumes1 := map[string]VolumeDevice{
				"vol-1": {VolumeHandle: "vol-1", Driver: "driver-a", Device: "sda", Node: "node1"},
				"vol-2": {VolumeHandle: "vol-2", Driver: "driver-b", Device: "sdb", Node: "node1"},
			}
			m.Reconcile(volumes1)

			volumes2 := map[string]VolumeDevice{
				"vol-1": {VolumeHandle: "vol-1", Driver: "driver-a", Device: "sda", Node: "node1"},
			}
			m.Reconcile(volumes2)

			count := testutil.CollectAndCount(m.volumeDeviceInfo)
			Expect(count).To(Equal(1))
		})

		It("should handle empty volumes", func() {
			volumes := map[string]VolumeDevice{
				"vol-1": {VolumeHandle: "vol-1", Driver: "driver-a", Device: "sda", Node: "node1"},
			}
			m.Reconcile(volumes)
			m.Reconcile(map[string]VolumeDevice{})

			count := testutil.CollectAndCount(m.volumeDeviceInfo)
			Expect(count).To(Equal(0))
		})

		It("should update volumes discovered per driver", func() {
			volumes := map[string]VolumeDevice{
				"vol-1": {VolumeHandle: "vol-1", Driver: "csi.trident.netapp.io", Device: "dm-0", Node: "node1"},
				"vol-2": {VolumeHandle: "vol-2", Driver: "csi.trident.netapp.io", Device: "dm-1", Node: "node1"},
				"vol-3": {VolumeHandle: "vol-3", Driver: "csi.hpe.com", Device: "dm-2", Node: "node1"},
			}
			m.Reconcile(volumes)

			expected := `
# HELP csi_volume_device_volumes_discovered Number of volumes discovered per driver.
# TYPE csi_volume_device_volumes_discovered gauge
csi_volume_device_volumes_discovered{driver="csi.hpe.com"} 1
csi_volume_device_volumes_discovered{driver="csi.trident.netapp.io"} 2
`
			Expect(testutil.CollectAndCompare(m.volumesDiscovered, strings.NewReader(expected))).To(Succeed())
		})
	})

	Describe("IncDiscoveryErrors", func() {
		It("should increment error counters", func() {
			m.IncDiscoveryErrors("trident")
			m.IncDiscoveryErrors("trident")
			m.IncDiscoveryErrors("hpe")

			expected := `
# HELP csi_volume_device_discovery_errors_total Total number of discovery errors by discoverer.
# TYPE csi_volume_device_discovery_errors_total counter
csi_volume_device_discovery_errors_total{discoverer="hpe"} 1
csi_volume_device_discovery_errors_total{discoverer="trident"} 2
`
			Expect(testutil.CollectAndCompare(m.discoveryErrors, strings.NewReader(expected))).To(Succeed())
		})
	})

	Describe("SetLastSuccessfulNow", func() {
		It("should update the gauge", func() {
			m.SetLastSuccessfulNow()

			gathered, err := m.registry.Gather()
			Expect(err).NotTo(HaveOccurred())

			found := false
			for _, mf := range gathered {
				if mf.GetName() == "csi_volume_device_last_discovery_timestamp_seconds" {
					found = true
					v := mf.GetMetric()[0].GetGauge().GetValue()
					Expect(v).To(BeNumerically(">", 0))
				}
			}
			Expect(found).To(BeTrue())
		})
	})
})
