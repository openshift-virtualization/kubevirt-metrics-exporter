package csi

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HPEDiscoverer", func() {
	var logger *slog.Logger

	BeforeEach(func() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	})

	It("should return empty when plugin dir does not exist", func() {
		d := NewHPEDiscoverer("/nonexistent", "/fake/sys", "node1", logger)

		results, err := d.Discover(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeEmpty())
	})

	It("should parse deviceInfo.json", func() {
		kubeletRoot := GinkgoT().TempDir()
		sysPath := GinkgoT().TempDir()

		pvDir := filepath.Join(kubeletRoot, "plugins", "kubernetes.io", "csi", "pvc-hpe-001", "globalmount")
		Expect(os.MkdirAll(pvDir, 0o755)).To(Succeed())

		content := `{
  "volume_id": "hpe-vol-123",
  "device": {
    "alt_full_path_name": "/dev/dm-7"
  }
}`
		Expect(os.WriteFile(filepath.Join(pvDir, "deviceInfo.json"), []byte(content), 0o644)).To(Succeed())
		setupSysForDM(sysPath, "dm-7", "mpath-hpe")

		d := NewHPEDiscoverer(kubeletRoot, sysPath, "node1", logger)
		results, err := d.Discover(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].VolumeHandle).To(Equal("hpe-vol-123"))
		Expect(results[0].Device).To(Equal("dm-7"))
		Expect(results[0].Driver).To(Equal("csi.hpe.com"))
	})

	It("should skip entries with missing fields", func() {
		kubeletRoot := GinkgoT().TempDir()

		pvDir := filepath.Join(kubeletRoot, "plugins", "kubernetes.io", "csi", "pvc-empty", "globalmount")
		Expect(os.MkdirAll(pvDir, 0o755)).To(Succeed())

		content := `{"volume_id": "", "device": {"alt_full_path_name": ""}}`
		Expect(os.WriteFile(filepath.Join(pvDir, "deviceInfo.json"), []byte(content), 0o644)).To(Succeed())

		d := NewHPEDiscoverer(kubeletRoot, "/fake/sys", "node1", logger)
		results, err := d.Discover(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeEmpty())
	})
})
