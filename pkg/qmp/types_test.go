package qmp

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testDomainXML = `<domain type='kvm'>
  <name>default_my-vm</name>
  <devices>
    <disk type='block' device='disk' model='virtio-non-transitional'>
      <driver name='qemu' type='raw'/>
      <source dev='/dev/vol-0'/>
      <target dev='vda' bus='virtio'/>
      <alias name='ua-vol-0'/>
      <address type='pci' domain='0x0000' bus='0x07' slot='0x00' function='0x0'/>
    </disk>
    <disk type='block' device='disk' model='virtio-non-transitional'>
      <driver name='qemu' type='raw'/>
      <source dev='/dev/vol-1'/>
      <target dev='vdb' bus='virtio'/>
      <alias name='ua-vol-1'/>
      <address type='pci' domain='0x0000' bus='0x08' slot='0x00' function='0x0'/>
    </disk>
    <disk type='block' device='disk' model='virtio-non-transitional'>
      <driver name='qemu' type='raw'/>
      <source dev='/dev/vol-2'/>
      <target dev='vdc' bus='virtio'/>
      <alias name='ua-vol-2'/>
      <address type='pci' domain='0x0000' bus='0x09' slot='0x00' function='0x0'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='/var/run/kubevirt-private/vmi-disks/cd-rom/disk.img'/>
      <target dev='sda' bus='sata'/>
      <alias name='ua-cd-rom'/>
      <address type='drive' controller='0' bus='0' target='0' unit='0'/>
    </disk>
    <controller type='scsi' index='0' model='virtio-non-transitional'>
      <alias name='scsi0'/>
      <address type='pci' domain='0x0000' bus='0x05' slot='0x00' function='0x0'/>
    </controller>
  </devices>
</domain>`

var _ = Describe("ParseDiskAddresses", func() {
	It("should parse PCI-addressed ua- disks", func() {
		result, err := ParseDiskAddresses(testDomainXML)
		Expect(err).NotTo(HaveOccurred())

		expected := map[PCIAddr]string{
			{Domain: 0, Bus: 7, Slot: 0, Function: 0}: "vol-0",
			{Domain: 0, Bus: 8, Slot: 0, Function: 0}: "vol-1",
			{Domain: 0, Bus: 9, Slot: 0, Function: 0}: "vol-2",
		}
		Expect(result).To(Equal(expected))
	})

	It("should skip cdrom devices", func() {
		result, err := ParseDiskAddresses(testDomainXML)
		Expect(err).NotTo(HaveOccurred())
		for _, name := range result {
			Expect(name).NotTo(Equal("cd-rom"))
		}
	})

	It("should skip disks with non-PCI addresses", func() {
		xml := `<domain><devices>
    <disk type='block' device='disk'>
      <alias name='ua-sata-disk'/>
      <address type='drive' controller='0' bus='0' target='0' unit='0'/>
    </disk>
  </devices></domain>`

		result, err := ParseDiskAddresses(xml)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeEmpty())
	})

	It("should skip disks without ua- alias", func() {
		xml := `<domain><devices>
    <disk type='block' device='disk'>
      <alias name='virtio-disk0'/>
      <address type='pci' domain='0x0000' bus='0x05' slot='0x00' function='0x0'/>
    </disk>
  </devices></domain>`

		result, err := ParseDiskAddresses(xml)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeEmpty())
	})

	It("should return empty map for empty devices", func() {
		result, err := ParseDiskAddresses("<domain><devices></devices></domain>")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeEmpty())
	})
})
