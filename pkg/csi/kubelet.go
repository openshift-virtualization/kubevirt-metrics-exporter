package csi

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type KubeletDiscoverer struct {
	kubeletRoot string
	sysPath     string
	nodeName    string
	logger      *slog.Logger
}

func NewKubeletDiscoverer(kubeletRoot, sysPath, nodeName string, logger *slog.Logger) *KubeletDiscoverer {
	return &KubeletDiscoverer{
		kubeletRoot: kubeletRoot,
		sysPath:     sysPath,
		nodeName:    nodeName,
		logger:      logger,
	}
}

func (d *KubeletDiscoverer) Name() string { return "kubelet" }

func (d *KubeletDiscoverer) Discover(ctx context.Context) ([]VolumeDevice, error) {
	seen := make(map[string]struct{})
	var results []VolumeDevice

	podsDir := filepath.Join(d.kubeletRoot, "pods")
	podEntries, err := os.ReadDir(podsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, podEntry := range podEntries {
		if ctx.Err() != nil {
			break
		}
		if !podEntry.IsDir() {
			continue
		}

		csiVolDir := filepath.Join(podsDir, podEntry.Name(), "volumes", "kubernetes.io~csi")
		volEntries, err := os.ReadDir(csiVolDir)
		if err != nil {
			continue
		}

		for _, volEntry := range volEntries {
			if ctx.Err() != nil {
				break
			}
			if !volEntry.IsDir() {
				continue
			}

			volDir := filepath.Join(csiVolDir, volEntry.Name())
			vd, err := readVolData(volDir)
			if err != nil {
				d.logger.Debug("skipping volume: cannot read vol_data.json",
					"pod", podEntry.Name(), "volume", volEntry.Name(), "error", err)
				continue
			}
			if vd.VolumeLifecycleMode == "Ephemeral" {
				d.logger.Debug("skipping ephemeral volume",
					"volume_handle", vd.VolumeHandle, "driver", vd.DriverName)
				continue
			}
			if _, ok := seen[vd.VolumeHandle]; ok {
				d.logger.Debug("skipping duplicate volume_handle",
					"volume_handle", vd.VolumeHandle)
				continue
			}

			device := d.resolveVolumeDevice(volDir)
			if device == "" {
				d.logger.Debug("skipping volume: device not resolvable (network FS or propagation issue)",
					"volume_handle", vd.VolumeHandle, "driver", vd.DriverName, "path", volDir)
				continue
			}

			seen[vd.VolumeHandle] = struct{}{}
			results = append(results, VolumeDevice{
				VolumeHandle: vd.VolumeHandle,
				Driver:       vd.DriverName,
				Device:       device,
				Node:         d.nodeName,
				PVName:       vd.SpecVolID,
			})
		}
	}

	if ctx.Err() != nil && len(results) == 0 {
		return nil, ctx.Err()
	}
	return results, nil
}

func (d *KubeletDiscoverer) resolveVolumeDevice(volDir string) string {
	mountDir := filepath.Join(volDir, "mount")
	return d.resolveFromFilesystemMount(mountDir)
}

func (d *KubeletDiscoverer) resolveFromFilesystemMount(path string) string {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return ""
	}

	var parentSt unix.Stat_t
	if err := unix.Stat(filepath.Dir(path), &parentSt); err != nil {
		return ""
	}
	if st.Dev == parentSt.Dev {
		return ""
	}

	return d.resolveAndFilter(unix.Major(st.Dev), unix.Minor(st.Dev))
}

func (d *KubeletDiscoverer) resolveAndFilter(major, minor uint32) string {
	device, err := ResolveDeviceName(d.sysPath, major, minor)
	if err != nil {
		return ""
	}

	if strings.HasPrefix(device, "dm-") {
		return ResolveMultipathDevice(d.sysPath, device)
	}
	return device
}

type volData struct {
	VolumeHandle        string `json:"volumeHandle"`
	DriverName          string `json:"driverName"`
	SpecVolID           string `json:"specVolID"`
	VolumeLifecycleMode string `json:"volumeLifecycleMode"`
}

func readVolData(dir string) (*volData, error) {
	data, err := readFileLimited(filepath.Join(dir, "vol_data.json"), maxJSONFileSize)
	if err != nil {
		return nil, err
	}
	var vd volData
	if err := json.Unmarshal(data, &vd); err != nil {
		return nil, err
	}
	if vd.VolumeHandle == "" || vd.DriverName == "" {
		return nil, ErrMissingFields
	}
	return &vd, nil
}
