package csi

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveDeviceName", func() {
	It("should resolve a symlink to a device name", func() {
		sysPath := GinkgoT().TempDir()
		blockDir := filepath.Join(sysPath, "dev", "block")
		Expect(os.MkdirAll(blockDir, 0o755)).To(Succeed())
		Expect(os.Symlink("../../block/dm-5", filepath.Join(blockDir, "253:5"))).To(Succeed())

		device, err := ResolveDeviceName(sysPath, 253, 5)
		Expect(err).NotTo(HaveOccurred())
		Expect(device).To(Equal("dm-5"))
	})

	It("should return an error for non-existent device", func() {
		sysPath := GinkgoT().TempDir()
		_, err := ResolveDeviceName(sysPath, 999, 999)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ResolveMultipathDevice", func() {
	It("should return non-DM devices unchanged", func() {
		device := ResolveMultipathDevice("/fake", "sda")
		Expect(device).To(Equal("sda"))
	})

	It("should return multipath devices as-is", func() {
		sysPath := GinkgoT().TempDir()
		setupSysForDM(sysPath, "dm-0", "mpath-12345")

		device := ResolveMultipathDevice(sysPath, "dm-0")
		Expect(device).To(Equal("dm-0"))
	})

	It("should find multipath in immediate slaves (LUKS over multipath)", func() {
		sysPath := GinkgoT().TempDir()

		setupSysForDM(sysPath, "dm-1", "CRYPT-LUKS2-xxx")
		setupSysForDM(sysPath, "dm-0", "mpath-abcdef")

		slavesDir := filepath.Join(sysPath, "block", "dm-1", "slaves")
		Expect(os.MkdirAll(slavesDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(slavesDir, "dm-0"), 0o755)).To(Succeed())

		device := ResolveMultipathDevice(sysPath, "dm-1")
		Expect(device).To(Equal("dm-0"))
	})

	It("should return device as-is when no multipath found in slaves", func() {
		sysPath := GinkgoT().TempDir()

		setupSysForDM(sysPath, "dm-2", "LVM-xxx")
		slavesDir := filepath.Join(sysPath, "block", "dm-2", "slaves")
		Expect(os.MkdirAll(slavesDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(slavesDir, "sda1"), 0o755)).To(Succeed())

		device := ResolveMultipathDevice(sysPath, "dm-2")
		Expect(device).To(Equal("dm-2"))
	})

	It("should return device as-is when slaves dir doesn't exist", func() {
		sysPath := GinkgoT().TempDir()
		setupSysForDM(sysPath, "dm-3", "LVM-xxx")

		device := ResolveMultipathDevice(sysPath, "dm-3")
		Expect(device).To(Equal("dm-3"))
	})
})

func setupSysForDM(sysPath, device, uuid string) {
	dmDir := filepath.Join(sysPath, "block", device, "dm")
	ExpectWithOffset(1, os.MkdirAll(dmDir, 0o755)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(filepath.Join(dmDir, "uuid"), []byte(uuid+"\n"), 0o644)).To(Succeed())
}
