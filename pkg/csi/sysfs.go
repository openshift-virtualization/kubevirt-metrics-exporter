package csi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxSysfsFileSize = 256

func ResolveDeviceName(sysPath string, major, minor uint32) (string, error) {
	link := filepath.Join(sysPath, "dev", "block", fmt.Sprintf("%d:%d", major, minor))
	target, err := os.Readlink(link)
	if err != nil {
		return "", fmt.Errorf("readlink %s: %w", link, err)
	}
	return filepath.Base(target), nil
}

// ResolveMultipathDevice checks if a DM device is a multipath device or has
// a multipath device as an immediate slave (one level, e.g. LUKS over
// multipath). Returns the multipath device name if found, otherwise returns
// the input device as-is.
func ResolveMultipathDevice(sysPath string, device string) string {
	if !strings.HasPrefix(device, "dm-") {
		return device
	}

	isMpath, err := isMultipathDevice(sysPath, device)
	if err != nil {
		return device
	}
	if isMpath {
		return device
	}

	slavesDir := filepath.Join(sysPath, "block", device, "slaves")
	entries, err := os.ReadDir(slavesDir)
	if err != nil {
		return device
	}

	for _, entry := range entries {
		slave := entry.Name()
		if !strings.HasPrefix(slave, "dm-") {
			continue
		}
		slaveIsMpath, err := isMultipathDevice(sysPath, slave)
		if err != nil {
			continue
		}
		if slaveIsMpath {
			return slave
		}
	}

	return device
}

func isMultipathDevice(sysPath string, device string) (bool, error) {
	uuidPath := filepath.Join(sysPath, "block", device, "dm", "uuid")
	data, err := readFileLimited(uuidPath, maxSysfsFileSize)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(string(data)), "mpath-"), nil
}
