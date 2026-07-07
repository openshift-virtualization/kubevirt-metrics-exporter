package csi

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TridentDiscoverer", func() {
	var logger *slog.Logger

	BeforeEach(func() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	})

	It("should be disabled when tracking dir is missing", func() {
		d := NewTridentDiscoverer("/nonexistent", "/fake/sys", "node1", logger)
		Expect(d.disabled).To(BeTrue())

		results, err := d.Discover(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeEmpty())
	})

	It("should parse tracking files", func() {
		trackingDir := GinkgoT().TempDir()
		sysPath := GinkgoT().TempDir()

		content := `{
  "volumePublishInfo": {
    "devicePath": "/dev/dm-3"
  },
  "stagingTargetPath": "/var/lib/kubelet/plugins/kubernetes.io/csi/pvc-123/globalmount"
}`
		Expect(os.WriteFile(filepath.Join(trackingDir, "vol-001.json"), []byte(content), 0o644)).To(Succeed())
		setupSysForDM(sysPath, "dm-3", "mpath-xyz")

		d := NewTridentDiscoverer(trackingDir, sysPath, "node1", logger)
		results, err := d.Discover(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].VolumeHandle).To(Equal("vol-001"))
		Expect(results[0].Device).To(Equal("dm-3"))
		Expect(results[0].Driver).To(Equal("csi.trident.netapp.io"))
	})

	It("should skip files with empty device path", func() {
		trackingDir := GinkgoT().TempDir()

		content := `{"volumePublishInfo": {}, "stagingTargetPath": "/mnt"}`
		Expect(os.WriteFile(filepath.Join(trackingDir, "vol-empty.json"), []byte(content), 0o644)).To(Succeed())

		d := NewTridentDiscoverer(trackingDir, "/fake/sys", "node1", logger)
		results, err := d.Discover(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeEmpty())
	})

	It("should respect context cancellation", func() {
		trackingDir := GinkgoT().TempDir()

		for i := range 5 {
			content := `{"volumePublishInfo": {"devicePath": "/dev/sda"}, "stagingTargetPath": "/mnt"}`
			name := filepath.Join(trackingDir, "vol-"+string(rune('a'+i))+".json")
			Expect(os.WriteFile(name, []byte(content), 0o644)).To(Succeed())
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		d := NewTridentDiscoverer(trackingDir, "/fake/sys", "node1", logger)
		_, err := d.Discover(ctx)
		Expect(err).To(MatchError(context.Canceled))
	})
})
