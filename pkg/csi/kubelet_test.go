package csi

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func setupTestSysfs(sysDir, devMajMin, symlinkTarget, dmUUID string) {
	devBlockDir := filepath.Join(sysDir, "dev", "block")
	ExpectWithOffset(1, os.MkdirAll(devBlockDir, 0o755)).To(Succeed())
	ExpectWithOffset(1, os.Symlink(symlinkTarget, filepath.Join(devBlockDir, devMajMin))).To(Succeed())
	if dmUUID != "" {
		device := filepath.Base(symlinkTarget)
		dmDir := filepath.Join(sysDir, "block", device, "dm")
		ExpectWithOffset(1, os.MkdirAll(dmDir, 0o755)).To(Succeed())
		ExpectWithOffset(1, os.WriteFile(filepath.Join(dmDir, "uuid"), []byte(dmUUID+"\n"), 0o644)).To(Succeed())
	}
}

func createPodVolume(kubeletRoot, podUID, pvName string, vd volData) string {
	volDir := filepath.Join(kubeletRoot, "pods", podUID, "volumes", "kubernetes.io~csi", pvName)
	mountDir := filepath.Join(volDir, "mount")
	ExpectWithOffset(1, os.MkdirAll(mountDir, 0o755)).To(Succeed())
	data, _ := json.Marshal(vd)
	ExpectWithOffset(1, os.WriteFile(filepath.Join(volDir, "vol_data.json"), data, 0o644)).To(Succeed())
	return mountDir
}

var _ = Describe("KubeletDiscoverer", func() {
	var logger *slog.Logger

	BeforeEach(func() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	})

	Describe("resolveAndFilter", func() {
		It("should resolve a multipath device", func() {
			tmpDir := GinkgoT().TempDir()
			sysDir := filepath.Join(tmpDir, "sys")
			setupTestSysfs(sysDir, "253:5", "../../devices/virtual/block/dm-5", "mpath-360060160abc")

			d := NewKubeletDiscoverer(filepath.Join(tmpDir, "kubelet"), sysDir, "worker-1", logger)
			device := d.resolveAndFilter(253, 5)
			Expect(device).To(Equal("dm-5"))
		})

		It("should resolve LUKS over multipath", func() {
			tmpDir := GinkgoT().TempDir()
			sysDir := filepath.Join(tmpDir, "sys")

			setupTestSysfs(sysDir, "253:7", "../../devices/virtual/block/dm-7", "CRYPT-LUKS2-abc-luks")
			setupTestSysfs(sysDir, "253:5", "../../devices/virtual/block/dm-5", "mpath-360060160abc")

			slavesDir := filepath.Join(sysDir, "block", "dm-7", "slaves")
			Expect(os.MkdirAll(slavesDir, 0o755)).To(Succeed())
			Expect(os.Symlink(filepath.Join(sysDir, "block", "dm-5"), filepath.Join(slavesDir, "dm-5"))).To(Succeed())

			d := NewKubeletDiscoverer(filepath.Join(tmpDir, "kubelet"), sysDir, "worker-1", logger)
			device := d.resolveAndFilter(253, 7)
			Expect(device).To(Equal("dm-5"))
		})

		It("should resolve NVMe devices", func() {
			tmpDir := GinkgoT().TempDir()
			sysDir := filepath.Join(tmpDir, "sys")
			setupTestSysfs(sysDir, "259:1", "../../devices/pci0000:00/0000:00:1f.0/nvme/nvme0/nvme0n1", "")

			d := NewKubeletDiscoverer(filepath.Join(tmpDir, "kubelet"), sysDir, "worker-1", logger)
			device := d.resolveAndFilter(259, 1)
			Expect(device).To(Equal("nvme0n1"))
		})

		It("should return empty for NFS (major=0)", func() {
			tmpDir := GinkgoT().TempDir()
			sysDir := filepath.Join(tmpDir, "sys")

			d := NewKubeletDiscoverer(filepath.Join(tmpDir, "kubelet"), sysDir, "worker-1", logger)
			device := d.resolveAndFilter(0, 2923)
			Expect(device).To(BeEmpty())
		})
	})

	Describe("Discover", func() {
		It("should skip ephemeral volumes", func() {
			tmpDir := GinkgoT().TempDir()
			kubeletRoot := filepath.Join(tmpDir, "kubelet")
			sysDir := filepath.Join(tmpDir, "sys")

			vd := volData{
				VolumeHandle:        "pod-uid-123",
				DriverName:          "csi.example.com",
				SpecVolID:           "ephemeral-vol",
				VolumeLifecycleMode: "Ephemeral",
			}
			createPodVolume(kubeletRoot, "pod-uid-123", "ephemeral-vol", vd)

			d := NewKubeletDiscoverer(kubeletRoot, sysDir, "worker-1", logger)
			results, err := d.Discover(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should handle unresolvable mounts gracefully", func() {
			tmpDir := GinkgoT().TempDir()
			kubeletRoot := filepath.Join(tmpDir, "kubelet")
			sysDir := filepath.Join(tmpDir, "sys")

			vd := volData{
				VolumeHandle:        "shared-vol-handle",
				DriverName:          "csi-powerstore.dellemc.com",
				SpecVolID:           "pvc-shared",
				VolumeLifecycleMode: "Persistent",
			}
			createPodVolume(kubeletRoot, "pod-uid-aaa", "pvc-shared", vd)
			createPodVolume(kubeletRoot, "pod-uid-bbb", "pvc-shared", vd)

			d := NewKubeletDiscoverer(kubeletRoot, sysDir, "worker-1", logger)
			results, err := d.Discover(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should guard against missing mount propagation", func() {
			tmpDir := GinkgoT().TempDir()
			kubeletRoot := filepath.Join(tmpDir, "kubelet")
			sysDir := filepath.Join(tmpDir, "sys")

			vd := volData{
				VolumeHandle:        "vol-handle-prop",
				DriverName:          "csi-powerstore.dellemc.com",
				SpecVolID:           "pvc-prop",
				VolumeLifecycleMode: "Persistent",
			}
			createPodVolume(kubeletRoot, "pod-uid-prop", "pvc-prop", vd)
			setupTestSysfs(sysDir, "8:0", "../../devices/pci0000:00/block/sda", "")

			d := NewKubeletDiscoverer(kubeletRoot, sysDir, "worker-1", logger)
			results, err := d.Discover(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should return empty for non-existent pods dir", func() {
			tmpDir := GinkgoT().TempDir()
			kubeletRoot := filepath.Join(tmpDir, "kubelet")
			sysDir := filepath.Join(tmpDir, "sys")

			d := NewKubeletDiscoverer(kubeletRoot, sysDir, "worker-1", logger)
			results, err := d.Discover(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})
	})
})
