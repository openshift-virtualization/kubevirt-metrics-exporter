package device

import "testing"

func TestParseKubeletVolumePath(t *testing.T) {
	tests := []struct {
		name       string
		mountPoint string
		wantUID    string
		wantPV     string
		wantOK     bool
	}{
		{
			name:       "NFS volume",
			mountPoint: "/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/volumes/kubernetes.io~nfs/example",
			wantUID:    "6a76960a-a927-4211-96e6-1f187b126e90",
			wantPV:     "example",
			wantOK:     true,
		},
		{
			name:       "CSI volume with /mount suffix",
			mountPoint: "/var/lib/kubelet/pods/aabbccdd-1234-5678-9abc-def012345678/volumes/kubernetes.io~csi/pvc-deadbeef-0000-1111-2222-333344445555/mount",
			wantUID:    "aabbccdd-1234-5678-9abc-def012345678",
			wantPV:     "pvc-deadbeef-0000-1111-2222-333344445555",
			wantOK:     true,
		},
		{
			name:       "local volume",
			mountPoint: "/var/lib/kubelet/pods/11111111-2222-3333-4444-555566667777/volumes/kubernetes.io~local-volume/local-pv-abc",
			wantUID:    "11111111-2222-3333-4444-555566667777",
			wantPV:     "local-pv-abc",
			wantOK:     true,
		},
		{
			name:       "non-kubelet mount",
			mountPoint: "/mnt/data",
			wantOK:     false,
		},
		{
			name:       "root filesystem",
			mountPoint: "/",
			wantOK:     false,
		},
		{
			name:       "kubelet path without volume",
			mountPoint: "/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/containers/app",
			wantOK:     false,
		},
		{
			name:       "invalid UUID in path",
			mountPoint: "/var/lib/kubelet/pods/not-a-uuid/volumes/kubernetes.io~nfs/example",
			wantOK:     false,
		},
		{
			name:       "block device path does not match filesystem regex",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/c68a7381-7595-4de4-a594-cffe21c042fe",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, pv, ok := parseKubeletVolumePath(tt.mountPoint)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if uid != tt.wantUID {
				t.Errorf("podUID = %q, want %q", uid, tt.wantUID)
			}
			if pv != tt.wantPV {
				t.Errorf("pvName = %q, want %q", pv, tt.wantPV)
			}
		})
	}
}

func TestParseKubeletBlockDevicePath(t *testing.T) {
	tests := []struct {
		name       string
		mountPoint string
		wantUID    string
		wantPV     string
		wantOK     bool
	}{
		{
			name:       "CSI block volume publish path",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/c68a7381-7595-4de4-a594-cffe21c042fe",
			wantUID:    "c68a7381-7595-4de4-a594-cffe21c042fe",
			wantPV:     "pvc-30f5aca9-100b-4759-b9d9-5d896081fd23",
			wantOK:     true,
		},
		{
			name:       "staging path (no pod UID)",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/staging/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/0001-0009-rook-ceph-0000000000000001-474bbf8f-2bec-4025-8391-5b316cdb801f",
			wantOK:     false,
		},
		{
			name:       "dev path (no pod UID at end)",
			mountPoint: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/pvc-30f5aca9-100b-4759-b9d9-5d896081fd23/dev/c68a7381-7595-4de4-a594-cffe21c042fe",
			wantOK:     false,
		},
		{
			name:       "filesystem volume path does not match block regex",
			mountPoint: "/var/lib/kubelet/pods/6a76960a-a927-4211-96e6-1f187b126e90/volumes/kubernetes.io~nfs/example",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, pv, ok := parseKubeletBlockDevicePath(tt.mountPoint)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if uid != tt.wantUID {
				t.Errorf("podUID = %q, want %q", uid, tt.wantUID)
			}
			if pv != tt.wantPV {
				t.Errorf("pvName = %q, want %q", pv, tt.wantPV)
			}
		})
	}
}
